package logs

import (
	"context"
	"testing"
)

func TestReadBack(t *testing.T) {
	f := &readLog{}
	f.write("l1\nl2\nl3\nl4\n")
	ctx := context.Background()
	text, start, err := ReadBack(ctx, f, "p", Stdout, -1, 7)
	if err != nil || text != "l3\nl4\n" || start != 6 {
		t.Fatalf("the end: %q from %d, %v", text, start, err)
	}
	text, start, err = ReadBack(ctx, f, "p", Stdout, start, 100)
	if err != nil || text != "l1\nl2\n" || start != 0 {
		t.Fatalf("before 6: %q from %d, %v", text, start, err)
	}
	if text, start, err = ReadBack(ctx, f, "p", Stdout, 0, 100); err != nil || text != "" || start != 0 {
		t.Fatalf("before 0: %q from %d, %v", text, start, err)
	}
	// One line longer than n: kept as read.
	f2 := &readLog{}
	f2.write("abcdefghij")
	if text, start, _ := ReadBack(ctx, f2, "p", Stdout, -1, 4); text != "ghij" || start != 6 {
		t.Fatalf("a long line: %q from %d", text, start)
	}
}
