package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

// notify delivers a best-effort message to every enabled channel that subscribed
// to this kind of event.
func (a *App) notify(ctx context.Context, run domain.Run, job domain.Job) {
	settings, err := a.Store.Settings().Get(ctx)
	if err != nil || len(settings.Notifications) == 0 {
		return
	}
	kind := "success"
	switch run.Status {
	case domain.StatusFailed:
		kind = "failure"
	case domain.StatusSuccess:
		kind = "change"
	}
	title := fmt.Sprintf("OzDDNS · %s · %s", job.Name, run.Status)
	body := run.Message
	if body == "" {
		body = string(run.Status)
	}
	if run.NewIP != "" && run.OldIP != "" && run.OldIP != run.NewIP {
		body = fmt.Sprintf("%s → %s (%s)", run.OldIP, run.NewIP, body)
	}
	for _, n := range settings.Notifications {
		if !n.Enabled || !subscribes(n.On, kind) {
			continue
		}
		if err := deliver(ctx, n, title, body); err != nil {
			a.Log.Warn("notification failed", "channel", n.Name, "err", err)
		}
	}
}

func subscribes(on []string, kind string) bool {
	if len(on) == 0 {
		return kind == "failure"
	}
	for _, o := range on {
		if o == kind || o == "all" {
			return true
		}
	}
	// "failure" subscribers also hear about "change"; "change" implies success.
	if kind == "failure" && len(on) == 0 {
		return true
	}
	return false
}

func deliver(ctx context.Context, n domain.Notification, title, body string) error {
	payload := map[string]any{"text": title + "\n" + body, "content": title + "\n" + body, "title": title, "message": body}
	b, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Target, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// TestNotification sends a sample message to one channel.
func (a *App) TestNotification(ctx context.Context, id string) error {
	settings, err := a.Store.Settings().Get(ctx)
	if err != nil {
		return err
	}
	for _, n := range settings.Notifications {
		if n.ID == id || id == "" {
			return deliver(ctx, n, "OzDDNS test", "If you can read this, the channel works.")
		}
	}
	return fmt.Errorf("notification channel %q not found", id)
}

// summarize is a tiny helper used by the CLI.
func summarize(r domain.Run) string {
	parts := []string{string(r.Status)}
	if r.OldIP != "" || r.NewIP != "" {
		parts = append(parts, strings.Join([]string{r.OldIP, "->", r.NewIP}, " "))
	}
	if r.Message != "" {
		parts = append(parts, r.Message)
	}
	return strings.Join(parts, " · ")
}
