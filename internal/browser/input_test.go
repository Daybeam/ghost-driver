package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

type MockBrowser struct {
	cdpClient *MockCDPClient
}

func (m *MockBrowser) SetIndicatorLabel(ctx context.Context, label string) error { return nil }
func (m *MockBrowser) SetIndicatorColor(ctx context.Context, color string) error { return nil }
func (m *MockBrowser) CDPClient() cdp.CDPClient                                     { return m.cdpClient }
func (m *MockBrowser) EvalJS(ctx context.Context, js string) (json.RawMessage, error) {
	return nil, nil
}
func (m *MockBrowser) MarkDirty()              {}
func (m *MockBrowser) IsSystemDialogOpen() bool { return false }

func TestNewInput(t *testing.T) {
	input := NewInput(nil, InputModeL1)
	if input == nil {
		t.Fatal("expected input to be initialized")
	}
	if input.mode != InputModeL1 {
		t.Errorf("expected mode L1, got %v", input.mode)
	}
}

func TestClick(t *testing.T) {
	mockCDP := &MockCDPClient{}
	mockBrowser := &MockBrowser{cdpClient: mockCDP}
	input := NewInput(mockBrowser, InputModeL2)

	err := input.Click(context.Background(), 100, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockCDP.LastMethod != "Input.dispatchMouseEvent" {
		t.Errorf("expected Input.dispatchMouseEvent, got %s", mockCDP.LastMethod)
	}
}

func TestLongClick(t *testing.T) {
	mockCDP := &MockCDPClient{}
	mockBrowser := &MockBrowser{cdpClient: mockCDP}
	input := NewInput(mockBrowser, InputModeL2)

	err := input.LongClick(context.Background(), 100, 200, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockCDP.LastMethod != "Input.dispatchMouseEvent" {
		t.Errorf("expected Input.dispatchMouseEvent, got %s", mockCDP.LastMethod)
	}
}

func TestTypeTextShortASCIIUsesPerCharEvents(t *testing.T) {
	mockCDP := &MockCDPClient{}
	mockBrowser := &MockBrowser{cdpClient: mockCDP}
	input := NewInput(mockBrowser, InputModeL2)

	if err := input.TypeText(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 5 chars × (keyDown + keyUp) = 10 dispatchKeyEvent calls, no insertText.
	if mockCDP.CallCount != 10 {
		t.Errorf("expected 10 per-char calls, got %d", mockCDP.CallCount)
	}
	for _, m := range mockCDP.Methods {
		if m != "Input.dispatchKeyEvent" {
			t.Errorf("expected only dispatchKeyEvent for short ASCII, got %s", m)
		}
	}
}

func TestTypeTextLongBatchesIntoOneCall(t *testing.T) {
	mockCDP := &MockCDPClient{}
	mockBrowser := &MockBrowser{cdpClient: mockCDP}
	input := NewInput(mockBrowser, InputModeL2)

	long := strings.Repeat("a", typeBatchThreshold+10)
	if err := input.TypeText(context.Background(), long); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Long text → single Input.insertText call.
	if mockCDP.CallCount != 1 {
		t.Errorf("expected 1 batched call, got %d", mockCDP.CallCount)
	}
	if mockCDP.LastMethod != "Input.insertText" {
		t.Errorf("expected Input.insertText, got %s", mockCDP.LastMethod)
	}
}

func TestTypeTextNonASCIIShortUsesInsertTextPerRune(t *testing.T) {
	mockCDP := &MockCDPClient{}
	mockBrowser := &MockBrowser{cdpClient: mockCDP}
	input := NewInput(mockBrowser, InputModeL2)

	// 2 non-ASCII runes, under threshold → per-rune insertText (existing behavior).
	if err := input.TypeText(context.Background(), "你好"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockCDP.CallCount != 2 {
		t.Errorf("expected 2 per-rune insertText calls, got %d", mockCDP.CallCount)
	}
	for _, m := range mockCDP.Methods {
		if m != "Input.insertText" {
			t.Errorf("expected insertText for non-ASCII, got %s", m)
		}
	}
}
