package storage

import (
	"net/url"
	"testing"
)

func TestSQLiteURIKeepsDriveInPath(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{"/data/state.db", "file:///data/state.db"},
		{"C:/data/state.db", "file:///C:/data/state.db"},
		{"d:/data/state #?.db", "file:///d:/data/state%20%23%3F.db"},
		{"//server/share/state.db", "file:////server/share/state.db"},
	} {
		t.Run(test.path, func(t *testing.T) {
			got := sqliteURI(test.path)
			if got != test.want {
				t.Fatalf("sqliteURI(%q) = %q, want %q", test.path, got, test.want)
			}
			parsed, err := url.Parse(got)
			if err != nil || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				t.Fatalf("file path became URI authority, query or fragment: %q", got)
			}
		})
	}
}
