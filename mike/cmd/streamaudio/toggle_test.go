// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAutoStop(t *testing.T) {
	flagMaxAudio = 5
	if err := startRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if !isRecording {
		t.Fatalf("not recording")
	}
	time.Sleep(time.Duration(flagMaxAudio) * time.Second)
	time.Sleep(time.Second)
	if isRecording {
		t.Fatalf("still recording")
	}
}

func TestManualStop(t *testing.T) {
	flagMaxAudio = 5
	if err := startRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if !isRecording {
		t.Fatalf("not recording")
	}

	time.Sleep(time.Second)
	if err := stopRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if isRecording {
		t.Fatalf("still recording")
	}

	time.Sleep(time.Second)
	if err := startRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if !isRecording {
		t.Fatalf("not recording")
	}

	// we want to show that the first auto-stop has been cancelled by the manual stop.
	// so we wait just long enough that it would have been triggered by now, if it still existed.
	time.Sleep(3 * time.Second)
	if !isRecording {
		t.Fatalf("not recording")
	}

	// now we wait again, for the second auto-stop (which should not have been cancelled) to have been triggered.
	time.Sleep(2 * time.Second)
	if isRecording {
		t.Fatalf("still recording")
	}
}

func TestStopAudio(t *testing.T) {
	bs := 3
	go func() {
		stream(bs, fromMicMock)
	}()
	time.Sleep(time.Second)

	if err := startRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if !isRecording {
		t.Fatalf("not recording")
	}

	// wait for first audio buffer to get filled
	time.Sleep(time.Duration(bs) * time.Second)

	resp, err := http.Get("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("not a 200: %d", resp.StatusCode)
	}

	if err := stopRecording(); err != nil {
		if !errors.Is(err, noCoreURLErr) {
			t.Fatal(err)
		}
	}
	if isRecording {
		t.Fatalf("still recording")
	}

	// check that when we are stopped, our streamer does not serve anything other than 429 with -1 as duration
	resp, err = http.Get("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("not a 429: %d", resp.StatusCode)
	}
	retry := resp.Header.Get("Retry-After")
	if retry == "" {
		t.Fatalf("missing Retry-After header in audio response")
	}
	sleep, err := strconv.Atoi(retry)
	if err != nil {
		t.Fatalf("while converting Retry-After header: %v", err)
	}

	if sleep != -1 {
		t.Fatalf("not an infinite sleep: %d", sleep)
	}

}

func fromMicMock(output io.Writer) error {
	f, err := os.Open("/dev/random")
	if err != nil {
		return err
	}
	defer f.Close()

	// send enough data for the "audio buffer" of our streamer to get populated
	var buf []byte = make([]byte, 1024)
	for i := 0; i < 40; i++ {
		if _, err := f.Read(buf); err != nil {
			return err
		}
		if _, err := output.Write(buf); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}
