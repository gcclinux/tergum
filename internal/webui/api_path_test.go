package webui

import (
	"runtime"
	"testing"
)

func TestResolveDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		tests := []struct {
			dest     string
			original string
			want     string
		}{
			{"c:\\temp", "C:\\Users\\ricardo\\file.txt", "c:\\temp\\Users\\ricardo\\file.txt"},
			{"c:\\temp", "C:/Users/ricardo/file.txt", "c:\\temp\\Users\\ricardo\\file.txt"},
			{"c:\\temp", "\\\\server\\share\\file.txt", "c:\\temp\\file.txt"},
			{"", "C:\\Users\\ricardo\\file.txt", "C:\\Users\\ricardo\\file.txt"},
			{"c:\\temp", "\\Users\\ricardo\\file.txt", "c:\\temp\\Users\\ricardo\\file.txt"},
		}
		for _, tt := range tests {
			got := resolveDestination(tt.dest, tt.original)
			if got != tt.want {
				t.Errorf("resolveDestination(%q, %q) = %q; want %q", tt.dest, tt.original, got, tt.want)
			}
		}
	} else {
		tests := []struct {
			dest     string
			original string
			want     string
		}{
			{"/tmp", "/home/user/file.txt", "/tmp/home/user/file.txt"},
			{"", "/home/user/file.txt", "/home/user/file.txt"},
		}
		for _, tt := range tests {
			got := resolveDestination(tt.dest, tt.original)
			if got != tt.want {
				t.Errorf("resolveDestination(%q, %q) = %q; want %q", tt.dest, tt.original, got, tt.want)
			}
		}
	}
}
