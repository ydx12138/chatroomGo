package main

import "testing"

func TestCanEnterPrivateMode(t *testing.T) {
	cases := []struct {
		name     string
		sender   string
		target   string
		online   map[string]struct{}
		wantCode string
		wantPass bool
	}{
		{
			name:     "success",
			sender:   "alice",
			target:   "bob",
			online:   map[string]struct{}{"alice": {}, "bob": {}},
			wantPass: true,
		},
		{
			name:     "self",
			sender:   "alice",
			target:   "alice",
			online:   map[string]struct{}{"alice": {}},
			wantCode: "TARGET_SELF",
		},
		{
			name:     "offline",
			sender:   "alice",
			target:   "bob",
			online:   map[string]struct{}{"alice": {}},
			wantCode: "TARGET_NOT_FOUND",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			errCode := canEnterPrivateMode(tc.sender, tc.target, tc.online)
			if tc.wantPass && errCode != "" {
				t.Fatalf("expected success, got code %q", errCode)
			}
			if !tc.wantPass && errCode != tc.wantCode {
				t.Fatalf("expected code %q, got %q", tc.wantCode, errCode)
			}
		})
	}
}

func TestFormatUserList(t *testing.T) {
	users := []string{"bob", "alice", "lucy"}
	got := formatUserList(users)
	if got != "alice,bob,lucy" {
		t.Fatalf("expected sorted list, got %q", got)
	}
}

func TestIsExitCommand(t *testing.T) {
	if !isExitCommand("/exit") {
		t.Fatalf("expected /exit to be accepted")
	}
	if isExitCommand(" /exit ") {
		t.Fatalf("expected untrimmed input to be rejected")
	}
}
