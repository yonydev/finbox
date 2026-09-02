package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type User struct {
	ID int64 `json:"id"`
}
type Chat struct {
	ID int64 `json:"id"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
}

type Document struct {
	FileID   string `json:"file_id"`
	MIMEType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type Message struct {
	MessageID    int64       `json:"message_id"`
	From         *User       `json:"from"`
	Chat         Chat        `json:"chat"`
	Text         string      `json:"text"`
	Photo        []PhotoSize `json:"photo"`
	Document     *Document   `json:"document"`
	MediaGroupID string      `json:"media_group_id"`
	ReplyTo      *Message    `json:"reply_to_message"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}
type InlineKeyboard [][]Button

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// API is what the bot logic depends on; Client implements it, tests fake it.
type API interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, html string, kb *InlineKeyboard) (Message, error)
	EditMessageText(ctx context.Context, chatID, messageID int64, html string, kb *InlineKeyboard) error
	AnswerCallbackQuery(ctx context.Context, id string) error
	GetFile(ctx context.Context, fileID string) (File, error)
	Download(ctx context.Context, filePath string) ([]byte, error)
	SetMyCommands(ctx context.Context, cmds []BotCommand) error
}

type Client struct {
	token, base string
	hc          *http.Client
}

func NewClient(token string) *Client {
	return &Client{token: token, base: "https://api.telegram.org", hc: &http.Client{Timeout: 90 * time.Second}}
}

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(ctx context.Context, method string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST",
			fmt.Sprintf("%s/bot%s/%s", c.base, c.token, method), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.hc.Do(req)
		if err != nil {
			return err
		}
		var ar apiResp
		err = json.NewDecoder(resp.Body).Decode(&ar)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("telegram %s: bad response: %w", method, err)
		}
		if !ar.OK {
			if resp.StatusCode == 429 && ar.Parameters != nil && attempt == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(ar.Parameters.RetryAfter) * time.Second):
					continue
				}
			}
			return fmt.Errorf("telegram %s: %s", method, ar.Description)
		}
		if out != nil {
			return json.Unmarshal(ar.Result, out)
		}
		return nil
	}
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var ups []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec,
		"allowed_updates": []string{"message", "callback_query"},
	}, &ups)
	return ups, err
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, html string, kb *InlineKeyboard) (Message, error) {
	p := map[string]any{"chat_id": chatID, "text": html, "parse_mode": "HTML"}
	if kb != nil {
		p["reply_markup"] = map[string]any{"inline_keyboard": *kb}
	}
	var m Message
	err := c.call(ctx, "sendMessage", p, &m)
	return m, err
}

func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, html string, kb *InlineKeyboard) error {
	p := map[string]any{"chat_id": chatID, "message_id": messageID, "text": html, "parse_mode": "HTML"}
	if kb != nil {
		p["reply_markup"] = map[string]any{"inline_keyboard": *kb}
	}
	return c.call(ctx, "editMessageText", p, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, id string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id}, nil)
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	var f File
	err := c.call(ctx, "getFile", map[string]any{"file_id": fileID}, &f)
	return f, err
}

func (c *Client) Download(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/file/bot%s/%s", c.base, c.token, filePath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 21<<20))
}

func (c *Client) SetMyCommands(ctx context.Context, cmds []BotCommand) error {
	return c.call(ctx, "setMyCommands", map[string]any{"commands": cmds}, nil)
}
