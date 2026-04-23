package main

import (
	"DEMO2/TCPIP/protocol"
	"testing"
)

func TestTranslateAuthError(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: protocol.CodeNameExists, want: "用户名已存在"},
		{code: protocol.CodeUserNotFound, want: "用户不存在"},
		{code: protocol.CodePasswordIncorrect, want: "密码错误"},
		{code: protocol.CodeAlreadyOnline, want: "该用户已经在线"},
	}

	for _, tc := range tests {
		got := translateAuthError(tc.code)
		if got != tc.want {
			t.Fatalf("code %q: expected %q, got %q", tc.code, tc.want, got)
		}
	}
}

func TestRenderServerPacket(t *testing.T) {
	tests := []struct {
		name     string
		packet   protocol.Packet
		wantLine string
		wantExit bool
	}{
		{
			name:     "public",
			packet:   protocol.Packet{Cmd: protocol.CmdPublic, Target: "alice", Content: "hello"},
			wantLine: "[公聊] alice: hello",
		},
		{
			name:     "private",
			packet:   protocol.Packet{Cmd: protocol.CmdPrivate, Target: "bob", Content: "hi"},
			wantLine: "[私聊] bob: hi",
		},
		{
			name:     "private ack",
			packet:   protocol.Packet{Cmd: protocol.CmdPrivateAck, Target: "bob", Content: "hi"},
			wantLine: "[私聊] 你对 bob 说: hi",
		},
		{
			name:     "system",
			packet:   protocol.Packet{Cmd: protocol.CmdSystem, Message: "注册成功"},
			wantLine: "[系统] 注册成功",
		},
		{
			name:     "shutdown",
			packet:   protocol.Packet{Cmd: protocol.CmdShutdown, Message: "服务器已关闭"},
			wantLine: "[系统] 服务器已关闭",
			wantExit: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			line, shouldExit := renderServerPacket(tc.packet)
			if line != tc.wantLine {
				t.Fatalf("expected %q, got %q", tc.wantLine, line)
			}
			if shouldExit != tc.wantExit {
				t.Fatalf("expected exit %v, got %v", tc.wantExit, shouldExit)
			}
		})
	}
}

func TestApplyPrivateEnterResult(t *testing.T) {
	session := &clientSession{mode: protocol.ChatModePublic}
	result := privateEnterResult{ok: true, target: "bob"}

	applyPrivateEnterResult(session, result)

	if session.mode != protocol.ChatModePrivate {
		t.Fatalf("expected private mode, got %q", session.mode)
	}
	if session.privateTarget != "bob" {
		t.Fatalf("expected target bob, got %q", session.privateTarget)
	}
	if session.waitingPrivate {
		t.Fatalf("waiting flag should be cleared")
	}
}
