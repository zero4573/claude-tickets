package textdiff

import "testing"

func TestUnified(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"equal", "a\nb\n", "a\nb\n", ""},
		{"insertion", "a\nb\nc\n", "a\nb\nx\nc\n", `--- a
+++ b
@@ -1,3 +1,4 @@
 a
 b
+x
 c
`},
		{"deletion", "a\nb\nc\n", "a\nc\n", `--- a
+++ b
@@ -1,3 +1,2 @@
 a
-b
 c
`},
		{"change", "a\nb\nc\n", "a\nB\nc\n", `--- a
+++ b
@@ -1,3 +1,3 @@
 a
-b
+B
 c
`},
		{"no trailing newline", "a\nb", "a\nb\n", `--- a
+++ b
@@ -1,2 +1,2 @@
 a
-b
\ No newline at end of file
+b
`},
		{"empty old side", "", "a\n", `--- a
+++ b
@@ -0,0 +1 @@
+a
`},
		{"empty new side", "a\nb\n", "", `--- a
+++ b
@@ -1,2 +0,0 @@
-a
-b
`},
		// changes more than 2*context lines apart get hunks of their own
		{"two hunks", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "x\n2\n3\n4\n5\n6\n7\n8\n9\ny\n", `--- a
+++ b
@@ -1,4 +1,4 @@
-1
+x
 2
 3
 4
@@ -7,4 +7,4 @@
 7
 8
 9
-10
+y
`},
		{"one hunk when close", "1\n2\n3\n4\n5\n6\n7\n8\n", "x\n2\n3\n4\n5\n6\n7\ny\n", `--- a
+++ b
@@ -1,8 +1,8 @@
-1
+x
 2
 3
 4
 5
 6
 7
-8
+y
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Unified("a", "b", []byte(tt.a), []byte(tt.b), 3); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
