package notify

import "context"

type Message struct {
	To      []string
	Subject string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}