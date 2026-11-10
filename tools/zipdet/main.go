// Command zipdet writes the guard's Lambda package as a deterministic zip:
// one entry named bootstrap, a fixed modification time, fixed permissions,
// and no extra fields. The same bootstrap binary always gives the same zip
// bytes, whatever the file's own mtime or the machine's zip tool.
//
//	go run ./tools/zipdet -mtime <unix seconds> -o guard.zip path/to/bootstrap
package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	mtime := flag.Int64("mtime", 0, "modification time to record, in Unix seconds (use the release commit's time)")
	out := flag.String("o", "", "zip file to write")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 || *mtime <= 0 {
		fmt.Fprintln(os.Stderr, "usage: zipdet -mtime <unix seconds> -o out.zip bootstrap")
		os.Exit(2)
	}
	content, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := Zip("bootstrap", content, time.Unix(*mtime, 0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Zip returns a zip holding one executable file. Only the name, the content
// and mtime decide the bytes.
func Zip(name string, content []byte, mtime time.Time) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	w.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	// Set the legacy MS-DOS time fields directly. Setting Modified would add
	// an extended-timestamp extra field; leaving both empty would record no
	// time at all, which some tools show as 1980.
	h.ModifiedDate, h.ModifiedTime = dosTime(mtime.UTC())
	h.SetMode(0o755)
	f, err := w.CreateHeader(h)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func dosTime(t time.Time) (date, tod uint16) {
	if t.Year() < 1980 {
		t = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	date = uint16(t.Day() + int(t.Month())<<5 + (t.Year()-1980)<<9)
	tod = uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)
	return date, tod
}
