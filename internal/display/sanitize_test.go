package display

import "testing"

func TestText(t *testing.T) {
	got := Text("\x1b[31mred\x1b[0m\tline\r\nnext\x00")
	want := "red line  next "
	if got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}

func TestBlock(t *testing.T) {
	got := Block("\x1b]0;spoof\x07first\r\nsecond\tvalue")
	want := "first \nsecond value"
	if got != want {
		t.Fatalf("Block() = %q, want %q", got, want)
	}
}

func TestTextPreservesUnicode(t *testing.T) {
	const want = "agent ◇ café"
	if got := Text(want); got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}
