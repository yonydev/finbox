package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBURL          string
	BlobDir        string
	BotToken       string
	OpenAIKey      string
	OpenAIModel    string
	AllowedUserIDs []int64
	Loc            *time.Location
}

func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		DBURL:       getenv("FINBOX_DB_URL"),
		BlobDir:     getenv("FINBOX_BLOB_DIR"),
		BotToken:    getenv("TELEGRAM_BOT_TOKEN"),
		OpenAIKey:   getenv("OPENAI_API_KEY"),
		OpenAIModel: getenv("FINBOX_OPENAI_MODEL"),
	}
	if c.OpenAIModel == "" {
		c.OpenAIModel = "gpt-4o-mini"
	}
	if raw := strings.TrimSpace(getenv("TELEGRAM_ALLOWED_USER_IDS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("TELEGRAM_ALLOWED_USER_IDS: %q is not an integer", part)
			}
			c.AllowedUserIDs = append(c.AllowedUserIDs, id)
		}
	}
	tz := getenv("TZ")
	if tz == "" {
		tz = "America/Mexico_City"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("TZ: %w", err)
	}
	c.Loc = loc
	return c, nil
}
