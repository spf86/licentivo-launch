package storage

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMySQLConnectionPolicyOverridesConflictingDSNOptions(t *testing.T) {
	cfg, err := mysqlConfiguration("launch:private-sentinel@tcp(127.0.0.1:3306)/launch?parseTime=false&loc=Local&charset=latin1&collation=utf8mb4_bin&time_zone=%27%2B08%3A00%27&multiStatements=true&timeout=1m&readTimeout=1m&writeTimeout=1m")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ParseTime || cfg.Loc != time.UTC || cfg.Params["time_zone"] != "'+00:00'" || cfg.MultiStatements {
		t.Fatal("DSN options overrode the application time or statement policy")
	}
	if cfg.Timeout != 3*time.Second || cfg.ReadTimeout != 5*time.Second || cfg.WriteTimeout != 5*time.Second {
		t.Fatal("DSN options removed the bounded network timeouts")
	}
	query, err := url.ParseQuery(strings.SplitN(cfg.FormatDSN(), "?", 2)[1])
	if err != nil || query.Get("charset") != "utf8mb4" || query.Get("collation") != "utf8mb4_bin" {
		t.Fatal("connection charset was not fixed to utf8mb4 or an explicit collation was lost")
	}
}

func TestMySQLConnectionPolicyAppliesWithoutDSNParameters(t *testing.T) {
	cfg, err := mysqlConfiguration("launch:private-sentinel@tcp(127.0.0.1:3306)/launch")
	if err != nil {
		t.Fatal(err)
	}
	query, err := url.ParseQuery(strings.SplitN(cfg.FormatDSN(), "?", 2)[1])
	if err != nil || !cfg.ParseTime || cfg.Loc != time.UTC || query.Get("charset") != "utf8mb4" || cfg.Params["time_zone"] != "'+00:00'" {
		t.Fatal("minimal DSN did not receive the Unicode and UTC connection policy")
	}
	for _, dsn := range []string{"", "launch:private-sentinel@tcp(127.0.0.1:3306)/"} {
		if _, err := mysqlConfiguration(dsn); err == nil || strings.Contains(err.Error(), "private-sentinel") {
			t.Fatal("invalid DSN was accepted or its password was disclosed")
		}
	}
}
