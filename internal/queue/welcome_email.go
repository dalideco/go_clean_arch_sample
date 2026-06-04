package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/dali/go_clean_arch_sample/internal/log"
	"github.com/dali/go_clean_arch_sample/internal/usecase"
)

// TypeWelcomeEmail is the asynq task name for the welcome-email job.
const TypeWelcomeEmail = "email:welcome"

// WelcomeEmailPayload is the producer↔consumer contract. Only the UserID
// rides the wire; the worker re-reads the user from the DB so the email
// content can evolve without payload migration and so the worker tolerates
// row updates between enqueue and process.
type WelcomeEmailPayload struct {
	UserID uuid.UUID `json:"user_id"`
}

// -- Producer side ------------------------------------------------------

// EnqueueWelcomeEmail satisfies usecase.WelcomeEmailEnqueuer.
func (c *Client) EnqueueWelcomeEmail(ctx context.Context, userID uuid.UUID) error {
	payload, err := json.Marshal(WelcomeEmailPayload{UserID: userID})
	if err != nil {
		return fmt.Errorf("marshal welcome email payload: %w", err)
	}
	task := asynq.NewTask(TypeWelcomeEmail, payload)
	if _, err := c.c.EnqueueContext(ctx, task, asynq.Queue(QueueEmails)); err != nil {
		return fmt.Errorf("enqueue %s: %w", TypeWelcomeEmail, err)
	}
	return nil
}

// -- Consumer side ------------------------------------------------------

// WelcomeEmailHandler processes welcome-email tasks: looks up the user and
// (today) logs that it *would* send the email — actual delivery isn't
// implemented yet. Wiring real SMTP is a one-line change at the send site.
// Depends on usecase.UserRepository directly — read-only lookups don't need
// the full UserUseCase or its write-side producers.
type WelcomeEmailHandler struct {
	users usecase.UserRepository
}

func NewWelcomeEmailHandler(users usecase.UserRepository) *WelcomeEmailHandler {
	return &WelcomeEmailHandler{users: users}
}

// Handle is the asynq.HandlerFunc-compatible entrypoint registered on the
// ServeMux for TypeWelcomeEmail.
func (h *WelcomeEmailHandler) Handle(ctx context.Context, t *asynq.Task) error {
	var payload WelcomeEmailPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		// Malformed payload — retrying won't fix it.
		return fmt.Errorf("unmarshal payload: %w: %w", err, asynq.SkipRetry)
	}

	u, err := h.users.Get(ctx, payload.UserID)
	if err != nil {
		if errors.Is(err, usecase.ErrUserNotFound) {
			// User was deleted between enqueue and process; retrying won't help.
			log.Warn("welcome email: user not found, dropping",
				"user_id", payload.UserID)
			return fmt.Errorf("user %s not found: %w", payload.UserID, asynq.SkipRetry)
		}
		return fmt.Errorf("lookup user %s: %w", payload.UserID, err)
	}

	// TODO: wire real email delivery (SMTP / provider API) here. For now we
	// only log the intent so the async path stays observable end-to-end.
	log.Info("welcome email: delivery not implemented yet, would send",
		"to", u.Email, "user_id", u.ID)
	return nil
}
