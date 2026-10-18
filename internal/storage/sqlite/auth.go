package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/storage"
)

type authRepo struct{ d *DB }

// authEventRetention bounds the security audit table. Failed sign-ins are
// intentionally audited, so without a cap an attacker rotating source addresses
// could grow the database forever even though the panel only reads recent rows.
const authEventRetention = 10_000

func (r authRepo) User(ctx context.Context) (domain.AuthUser, error) {
	var (
		u       domain.AuthUser
		created int64
	)
	err := r.d.db.QueryRowContext(ctx,
		`SELECT username,password_hash,totp_secret,totp_last_step,created_at
		 FROM auth_user WHERE id=1`).
		Scan(&u.Username, &u.PasswordHash, &u.TOTPSecret, &u.TOTPLastStep, &created)
	if err == sql.ErrNoRows {
		return domain.AuthUser{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AuthUser{}, err
	}
	u.CreatedAt = fromMillis(created)
	return u, nil
}

func (r authRepo) CompleteSetup(ctx context.Context, u domain.AuthUser, codes []domain.RecoveryCode) error {
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_user WHERE id=1`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return storage.ErrConflict
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO auth_user (id,username,password_hash,totp_secret,totp_last_step,created_at)
		 VALUES (1,?,?,?,?,?)`,
		u.Username, u.PasswordHash, u.TOTPSecret, u.TOTPLastStep, toMillis(u.CreatedAt)); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO auth_recovery_codes (id,user_id,code_hash,used_at) VALUES (?,1,?,NULL)`,
			code.ID, code.CodeHash); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_pending WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r authRepo) ClearAuth(ctx context.Context) error {
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_pending WHERE id=1`); err != nil {
		return err
	}
	// Sessions, challenges and recovery codes cascade from auth_user.
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_user WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r authRepo) UseTOTPStep(ctx context.Context, step int64) (bool, error) {
	res, err := r.d.db.ExecContext(ctx,
		`UPDATE auth_user SET totp_last_step=? WHERE id=1 AND totp_last_step<?`, step, step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r authRepo) Pending(ctx context.Context) (domain.AuthPending, error) {
	var (
		p       domain.AuthPending
		created int64
		expires int64
	)
	err := r.d.db.QueryRowContext(ctx,
		`SELECT username,password_hash,totp_secret,created_at,expires_at
		 FROM auth_pending WHERE id=1`).
		Scan(&p.Username, &p.PasswordHash, &p.TOTPSecret, &created, &expires)
	if err == sql.ErrNoRows {
		return domain.AuthPending{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AuthPending{}, err
	}
	p.CreatedAt = fromMillis(created)
	p.ExpiresAt = fromMillis(expires)
	return p, nil
}

func (r authRepo) SavePending(ctx context.Context, p domain.AuthPending) error {
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO auth_pending (id,username,password_hash,totp_secret,created_at,expires_at)
		 VALUES (1,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   username=excluded.username,
		   password_hash=excluded.password_hash,
		   totp_secret=excluded.totp_secret,
		   created_at=excluded.created_at,
		   expires_at=excluded.expires_at`,
		p.Username, p.PasswordHash, p.TOTPSecret, toMillis(p.CreatedAt), toMillis(p.ExpiresAt))
	return err
}

func (r authRepo) DeletePending(ctx context.Context) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_pending WHERE id=1`)
	return err
}

func (r authRepo) Challenge(ctx context.Context, tokenHash string) (domain.AuthChallenge, error) {
	var (
		c       domain.AuthChallenge
		created int64
		expires int64
	)
	err := r.d.db.QueryRowContext(ctx,
		`SELECT token_hash,created_at,expires_at,attempts
		 FROM auth_challenges WHERE token_hash=?`, tokenHash).
		Scan(&c.TokenHash, &created, &expires, &c.Attempts)
	if err == sql.ErrNoRows {
		return domain.AuthChallenge{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AuthChallenge{}, err
	}
	c.CreatedAt = fromMillis(created)
	c.ExpiresAt = fromMillis(expires)
	return c, nil
}

func (r authRepo) SaveChallenge(ctx context.Context, c domain.AuthChallenge) error {
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO auth_challenges (token_hash,user_id,created_at,expires_at,attempts)
		 VALUES (?,1,?,?,?)
		 ON CONFLICT(token_hash) DO UPDATE SET expires_at=excluded.expires_at, attempts=excluded.attempts`,
		c.TokenHash, toMillis(c.CreatedAt), toMillis(c.ExpiresAt), c.Attempts)
	return err
}

func (r authRepo) ConsumeChallenge(ctx context.Context, tokenHash string) (bool, error) {
	res, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE token_hash=?`, tokenHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r authRepo) DeleteChallenge(ctx context.Context, tokenHash string) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE token_hash=?`, tokenHash)
	return err
}

func (r authRepo) DeleteExpiredChallenges(ctx context.Context, now time.Time) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_challenges WHERE expires_at<=?`, toMillis(now))
	return err
}

func (r authRepo) IncrementChallengeAttempts(ctx context.Context, tokenHash string) (int, error) {
	if _, err := r.d.db.ExecContext(ctx,
		`UPDATE auth_challenges SET attempts=attempts+1 WHERE token_hash=?`, tokenHash); err != nil {
		return 0, err
	}
	c, err := r.Challenge(ctx, tokenHash)
	if err != nil {
		return 0, err
	}
	return c.Attempts, nil
}

func (r authRepo) InsertSession(ctx context.Context, s domain.AuthSession) error {
	_, err := r.d.db.ExecContext(ctx,
		`INSERT INTO auth_sessions
		   (token_hash,user_id,created_at,last_seen_at,idle_expires_at,absolute_expires_at,user_agent,ip)
		 VALUES (?,1,?,?,?,?,?,?)`,
		s.TokenHash, toMillis(s.CreatedAt), toMillis(s.LastSeenAt),
		toMillis(s.IdleExpiresAt), toMillis(s.AbsoluteExpiresAt), s.UserAgent, s.IP)
	return err
}

func (r authRepo) Session(ctx context.Context, tokenHash string) (domain.AuthSession, error) {
	var (
		s        domain.AuthSession
		created  int64
		lastSeen int64
		idle     int64
		absolute int64
	)
	err := r.d.db.QueryRowContext(ctx,
		`SELECT token_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,user_agent,ip
		 FROM auth_sessions WHERE token_hash=?`, tokenHash).
		Scan(&s.TokenHash, &created, &lastSeen, &idle, &absolute, &s.UserAgent, &s.IP)
	if err == sql.ErrNoRows {
		return domain.AuthSession{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AuthSession{}, err
	}
	s.CreatedAt = fromMillis(created)
	s.LastSeenAt = fromMillis(lastSeen)
	s.IdleExpiresAt = fromMillis(idle)
	s.AbsoluteExpiresAt = fromMillis(absolute)
	return s, nil
}

func (r authRepo) TouchSession(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time) error {
	_, err := r.d.db.ExecContext(ctx,
		`UPDATE auth_sessions SET last_seen_at=?, idle_expires_at=? WHERE token_hash=?`,
		toMillis(lastSeen), toMillis(idleExpires), tokenHash)
	return err
}

func (r authRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (r authRepo) DeleteAllSessions(ctx context.Context) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_sessions`)
	return err
}

func (r authRepo) SessionCount(ctx context.Context) (int, error) {
	var count int
	err := r.d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_sessions`).Scan(&count)
	return count, err
}

func (r authRepo) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := r.d.db.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE absolute_expires_at<=? OR idle_expires_at<=?`,
		toMillis(now), toMillis(now))
	return err
}

func (r authRepo) InsertRecoveryCodes(ctx context.Context, codes []domain.RecoveryCode) error {
	if len(codes) == 0 {
		return nil
	}
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, code := range codes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO auth_recovery_codes (id,user_id,code_hash,used_at) VALUES (?,1,?,NULL)`,
			code.ID, code.CodeHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r authRepo) ReplaceRecoveryCodes(ctx context.Context, codes []domain.RecoveryCode) error {
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_recovery_codes`); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO auth_recovery_codes (id,user_id,code_hash,used_at) VALUES (?,1,?,NULL)`,
			code.ID, code.CodeHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r authRepo) ListRecoveryCodes(ctx context.Context) ([]domain.RecoveryCode, error) {
	rows, err := r.d.db.QueryContext(ctx,
		`SELECT id,code_hash,used_at FROM auth_recovery_codes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RecoveryCode
	for rows.Next() {
		var (
			code domain.RecoveryCode
			used sql.NullInt64
		)
		if err := rows.Scan(&code.ID, &code.CodeHash, &used); err != nil {
			return nil, err
		}
		if used.Valid {
			t := fromMillis(used.Int64)
			code.UsedAt = &t
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

func (r authRepo) UseRecoveryCode(ctx context.Context, id string, at time.Time) error {
	res, err := r.d.db.ExecContext(ctx,
		`UPDATE auth_recovery_codes SET used_at=? WHERE id=? AND used_at IS NULL`,
		toMillis(at), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r authRepo) DeleteRecoveryCodes(ctx context.Context) error {
	_, err := r.d.db.ExecContext(ctx, `DELETE FROM auth_recovery_codes`)
	return err
}

func (r authRepo) AddEvent(ctx context.Context, e domain.AuthEvent) error {
	tx, err := r.d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO auth_events (at,event,username,detail,ip,user_agent) VALUES (?,?,?,?,?,?)`,
		toMillis(e.At), e.Event, e.Username, e.Detail, e.IP, e.UserAgent); err != nil {
		return err
	}
	// Keep the newest N rows. If fewer exist, the subquery is NULL and the
	// COALESCE cutoff leaves every row intact.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM auth_events WHERE id < COALESCE(
			(SELECT id FROM auth_events ORDER BY id DESC LIMIT 1 OFFSET ?), 0)`,
		authEventRetention-1); err != nil {
		return err
	}
	return tx.Commit()
}

func (r authRepo) ListEvents(ctx context.Context, limit int) ([]domain.AuthEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.d.db.QueryContext(ctx,
		`SELECT id,at,event,username,detail,ip,user_agent
		 FROM auth_events ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AuthEvent
	for rows.Next() {
		var (
			e      domain.AuthEvent
			at     int64
			user   sql.NullString
			detail sql.NullString
			ip     sql.NullString
			agent  sql.NullString
		)
		if err := rows.Scan(&e.ID, &at, &e.Event, &user, &detail, &ip, &agent); err != nil {
			return nil, err
		}
		e.At = fromMillis(at)
		e.Username, e.Detail, e.IP, e.UserAgent = user.String, detail.String, ip.String, agent.String
		out = append(out, e)
	}
	return out, rows.Err()
}
