package domain

import "time"

// AuthUser is the single administrator account. Authorization is enabled
// exactly when this row exists; disabling it deletes the row (and, through
// foreign keys, every session, challenge and recovery code).
type AuthUser struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	TOTPSecret   string    `json:"-"`
	TOTPLastStep int64     `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AuthPending is an in-progress enrollment. It never grants access; it exists
// only between "start setup" and the first valid TOTP code.
type AuthPending struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	TOTPSecret   string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// AuthSession is a server-side browser session. Only the token hash is stored.
type AuthSession struct {
	TokenHash         string    `json:"-"`
	CreatedAt         time.Time `json:"createdAt"`
	LastSeenAt        time.Time `json:"lastSeenAt"`
	IdleExpiresAt     time.Time `json:"idleExpiresAt"`
	AbsoluteExpiresAt time.Time `json:"absoluteExpiresAt"`
	UserAgent         string    `json:"userAgent,omitempty"`
	IP                string    `json:"ip,omitempty"`
}

// AuthChallenge is the short-lived token handed out after a correct password
// and before a valid TOTP/recovery code.
type AuthChallenge struct {
	TokenHash string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Attempts  int       `json:"attempts"`
}

// RecoveryCode is a hashed one-time fallback for a lost authenticator.
type RecoveryCode struct {
	ID       string     `json:"id"`
	CodeHash string     `json:"-"`
	UsedAt   *time.Time `json:"usedAt,omitempty"`
}

// AuthEvent is an append-only audit entry for security-relevant actions.
type AuthEvent struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Event     string    `json:"event"`
	Username  string    `json:"username,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`
}
