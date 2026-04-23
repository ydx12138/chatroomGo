package protocol

import "testing"

func TestValidateUsername(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "alice", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "too long", input: "abcdefghijk", wantErr: true},
		{name: "space", input: "alice bob", wantErr: true},
		{name: "delimiter", input: "alice|bob", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUsername(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "123456", wantErr: false},
		{name: "too short", input: "12345", wantErr: true},
		{name: "too long", input: "12345678901", wantErr: true},
		{name: "space", input: "123 456", wantErr: true},
		{name: "delimiter", input: "123|456", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.input, err)
			}
		})
	}
}

func TestParseClientPacket(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantCmd     string
		wantTarget  string
		wantContent string
	}{
		{
			name:        "public chat",
			raw:         "PUBLIC|hello world",
			wantCmd:     CmdPublic,
			wantContent: "hello world",
		},
		{
			name:       "private enter",
			raw:        "PRIVATE_ENTER|bob",
			wantCmd:    CmdPrivateEnter,
			wantTarget: "bob",
		},
		{
			name:        "private message",
			raw:         "PRIVATE|bob|hello | there",
			wantCmd:     CmdPrivate,
			wantTarget:  "bob",
			wantContent: "hello | there",
		},
		{
			name:    "list",
			raw:     "LIST",
			wantCmd: CmdList,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			packet, err := ParseClientPacket(tc.raw)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if packet.Cmd != tc.wantCmd {
				t.Fatalf("expected cmd %q, got %+v", tc.wantCmd, packet)
			}
			if packet.Target != tc.wantTarget {
				t.Fatalf("expected target %q, got %q", tc.wantTarget, packet.Target)
			}
			if packet.Content != tc.wantContent {
				t.Fatalf("expected content %q, got %q", tc.wantContent, packet.Content)
			}
		})
	}
}

func TestParseServerPacket(t *testing.T) {
	packet, err := ParseServerPacket("PRIVATE_ACK|bob|hi")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if packet.Cmd != CmdPrivateAck {
		t.Fatalf("unexpected packet: %+v", packet)
	}
	if packet.Target != "bob" || packet.Content != "hi" {
		t.Fatalf("unexpected target/content: %+v", packet)
	}
}

func TestParseChatInput(t *testing.T) {
	tests := []struct {
		name       string
		mode       ChatMode
		input      string
		wantAction string
		wantTarget string
		wantText   string
		wantErr    bool
	}{
		{
			name:       "public text",
			mode:       ChatModePublic,
			input:      "hello",
			wantAction: CmdPublic,
			wantText:   "hello",
		},
		{
			name:       "enter private",
			mode:       ChatModePublic,
			input:      "/chat bob",
			wantAction: CmdPrivateEnter,
			wantTarget: "bob",
		},
		{
			name:       "list",
			mode:       ChatModePublic,
			input:      "/list",
			wantAction: CmdList,
		},
		{
			name:       "private exit",
			mode:       ChatModePrivate,
			input:      "/exit",
			wantAction: InputLeavePrivate,
		},
		{
			name:    "nested private rejected",
			mode:    ChatModePrivate,
			input:   "/chat lucy",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := ParseChatInput(tc.mode, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Action != tc.wantAction {
				t.Fatalf("expected action %q, got %q", tc.wantAction, cmd.Action)
			}
			if cmd.Target != tc.wantTarget {
				t.Fatalf("expected target %q, got %q", tc.wantTarget, cmd.Target)
			}
			if cmd.Text != tc.wantText {
				t.Fatalf("expected text %q, got %q", tc.wantText, cmd.Text)
			}
		})
	}
}
