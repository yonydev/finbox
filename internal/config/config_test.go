package config

import "testing"

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnv(t *testing.T) {
	c, err := FromEnv(fakeEnv(map[string]string{
		"FINBOX_DB_URL":             "postgres://x",
		"FINBOX_BLOB_DIR":           "/data/receipts",
		"TELEGRAM_BOT_TOKEN":        "tok",
		"TELEGRAM_ALLOWED_USER_IDS": "111, 222",
		"OPENAI_API_KEY":            "sk-x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenAIModel != "gpt-4o-mini" {
		t.Errorf("default model = %q", c.OpenAIModel)
	}
	if len(c.AllowedUserIDs) != 2 || c.AllowedUserIDs[1] != 222 {
		t.Errorf("allowed = %v", c.AllowedUserIDs)
	}
	if c.Loc.String() != "America/Mexico_City" {
		t.Errorf("loc = %v", c.Loc)
	}
}

func TestFromEnvBadID(t *testing.T) {
	_, err := FromEnv(fakeEnv(map[string]string{"TELEGRAM_ALLOWED_USER_IDS": "abc"}))
	if err == nil {
		t.Fatal("want error for non-numeric id")
	}
}
