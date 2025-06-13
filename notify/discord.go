// notify/discord.go
package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

type Notifier struct {
	webhook string
}

func New(url string) *Notifier {
	return &Notifier{webhook: url}
}

func (n *Notifier) Send(status, message string) {
	if n.webhook == "" {
		return
	}
	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       status,
				"description": message,
				"color":       3066993,
			},
		},
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(n.webhook, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("Failed to send Discord message: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Sent notification: %s", message)
}
