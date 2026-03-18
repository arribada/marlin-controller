// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/yobert/alsa"
)

var (
	flagHost     string
	flagCoreURL  string
	flagVerbose  bool
	flagDebug    bool
	flagMaxAudio int
)

func init() {
	flag.StringVar(&flagHost, "host", ":7142", "host:port on which the audio is served")
	flag.BoolVar(&flagVerbose, "v", false, "be verbose")
	flag.BoolVar(&flagDebug, "debug", false, "enable debugging helpers. unrelated to logging.")
	flag.StringVar(&flagCoreURL, "core", "", "where to forward the start/stop signals to, to stop the audio transcribing")
	flag.IntVar(&flagMaxAudio, "max_audio", 600, "maximum duration for audio capture")
}

const (
	// alsa stuff
	// rawRate     = 44100
	rawRate     = 16000
	numChannels = 1
	bitDepth    = 16
	rate        = 16000
	format      = alsa.S16_LE
	sampleBytes = 2
	// TODO: revisit this. And I don't really trust/understand the upstream code for it.
	alsaBufferSize1 = 8192
	alsaBufferSize2 = 16384
	recDuration     = time.Second

	// server stuff
	// default length of audio (in seconds) that we serve for each HTTP request.
	defaultBufferSize = 20
	// arbitrary.
	bufferChunk = 4096
)

var (
	stateMu sync.RWMutex
	// even though we're actually always reading audio from the microphone,
	// isRecording is what keeps us consistent about our state all throughout
	// the program. it also informs our HTTP server whether we are supposed
	// to serve the audio or not.
	isRecording bool
	isErroring  bool
	// to automatically stop the audio recording after flagMaxAudio,
	// if it hasn't already been done manually.
	autostop *time.Timer
)

func checkFlags() {
	if flagMaxAudio <= 0 {
		log.Fatalf("max_audio cannot be negative")
	}
}

func main() {
	flag.Parse()

	checkFlags()

	if err := setLED(false, false); err != nil {
		log.Fatal(err)
	}

	if flagDebug {
		go handleCLISignals()
	}
	go func() {
		if err := handleButtons(); err != nil {
			log.Printf("handling of start/stop buttons failed: %v", err)
			// let the streaming continue nonetheless
		}
	}()

	stream(defaultBufferSize, fromMic)
}

func stream(bufferSize int, sourceFunc func(output io.Writer) error) {
	pr, pw := io.Pipe()
	bs := time.Duration(bufferSize) * time.Second
	as := &audioStreamer{
		source:      pr,
		bufferSize:  bs,
		bufferChunk: bufferChunk,
		nextPart:    time.Now().Add(bs),
	}
	go func() {
		if err := sourceFunc(pw); err != nil {
			log.Fatal(err)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		as.populate()
	}()

	if flagDebug {
		dbg := &debug{}
		http.Handle("/debug/", dbg)
	}
	http.Handle("/", as)
	log.Fatal(http.ListenAndServe(flagHost, nil))
}

func device() (*alsa.Device, error) {
	cards, err := alsa.OpenCards()
	if err != nil {
		return nil, err
	}
	defer alsa.CloseCards(cards)

	for _, card := range cards {
		println("CARD: ", card.Path, card.Title)
		devices, err := card.Devices()
		if err != nil {
			return nil, err
		}
		for _, device := range devices {
			println("DEVICE: ", device.Path, device.Title)
			if device.Type != alsa.PCM {
				continue
			}
			if device.Record {
				return device, nil
			}
		}
	}
	return nil, errors.New("no recording device found")
}

func processAudio(ctx context.Context, output io.Writer, workC chan []byte) error {
	inc := binary.Size(int16(0))
	// resampling not needed anymore, since we capture at 16KHz already
	/*
		r, err := gomplerate.NewResampler(numChannels, rawRate, rate)
		if err != nil {
			return fmt.Errorf("while initializing resampler: %v", err)
		}
	*/
	for {
		var work []byte
		select {
		case <-ctx.Done():
			return nil
		case work = <-workC:
		}

		// extract int(16)s from the bytes stream, and resample them
		var off int
		sampleCount := len(work) / sampleBytes
		data := make([]int16, sampleCount)
		for i := 0; i < sampleCount; i++ {
			data[i] = int16(binary.LittleEndian.Uint16(work[off:]))
			off += inc
		}
		// sampled := r.ResampleInt16(data)
		sampled := data

		// and encode ints again (arbitrarily with varint) as byte stream so we can send
		// over the wire.
		var sampleAsBytes []byte
		for _, v := range sampled {
			sampleAsBytes = binary.AppendVarint(sampleAsBytes, int64(v))
		}

		// pipe byte stream to the audio server
		n, err := output.Write(sampleAsBytes)
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("while writing audio out: %v", err)
			}
			return fmt.Errorf("TODO: short write, save the remainder: %d", n)
		}
	}
}

func prepare(rec *alsa.Device) error {
	if _, err := rec.NegotiateChannels(numChannels); err != nil {
		return err
	}

	if _, err := rec.NegotiateRate(rawRate); err != nil {
		return err
	}

	if _, err := rec.NegotiateFormat(format); err != nil {
		return err
	}

	if _, err := rec.NegotiateBufferSize(alsaBufferSize1, alsaBufferSize2); err != nil {
		return err
	}

	return rec.Prepare()
}

func recordAudio(ctx context.Context, rec *alsa.Device, workC chan []byte) {
	buf := rec.NewBufferDuration(recDuration)
	for {
		// No need to zero out buf.Data for reuse, it's all getting properly overwritten
		// by Read.
		if err := rec.Read(buf.Data); err != nil {
			if flagVerbose {
				log.Printf("input audio frames were dropped: %v", err)
			}
			continue
		}

		// send the audio chunk as fast as possible, to be dealt with asynchronously, so
		// we can go back to actually fetching some audio ASAP.
		select {
		case <-ctx.Done():
			return
		case workC <- buf.Data:
		default:
			if flagVerbose {
				log.Printf("audio chan too busy, dropping audio frames")
			}
		}
	}
}

type audioStreamer struct {
	muPart sync.RWMutex
	// data served
	currentPart []byte
	// time when the next chunk of data will be ready to be served.
	nextPart time.Time

	// audio source that we fill the served buffer with.
	source io.Reader
	// length of audio that we serve for each HTTP request.
	bufferSize time.Duration
	// size of of a read in the tight read loop that fills the buffer with data.
	bufferChunk int
}

func (as *audioStreamer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	stateMu.RLock()
	isRec := isRecording
	stateMu.RUnlock()
	// tell the core we're not recording, so it's not supposed to fetch from us
	if !isRec {
		retryDuration := -1
		w.Header().Add("Retry-After", fmt.Sprintf("%d", retryDuration))
		w.WriteHeader(429)
		return
	}

	as.muPart.RLock()
	if as.currentPart == nil {
		retryDuration := time.Until(as.nextPart) / time.Second
		w.Header().Add("Retry-After", fmt.Sprintf("%d", retryDuration))
		w.WriteHeader(429)
		as.muPart.RUnlock()
		return
	}
	// TODO: any headers, chunk encoding?
	_, err := w.Write(as.currentPart)
	if err != nil {
		log.Printf("error serving audio chunk: %v", err)
		as.muPart.RUnlock()
		return
	}
	as.muPart.RUnlock()

	as.muPart.Lock()
	if flagVerbose {
		log.Printf("served %d bytes audio chunk", len(as.currentPart))
	}
	// to make sure we don't ever serve twice the same. A bit brutal, but oh well.
	as.currentPart = nil
	as.muPart.Unlock()
}

func (as *audioStreamer) populate() {
	tempDir := os.TempDir()
	if flagDebug {
		log.Printf("debug wav file will be written in: %v", tempDir)
	}
	wavCount := 1
	for {
		stopTime := time.Now().Add(as.bufferSize)
		as.muPart.Lock()
		as.nextPart = stopTime
		as.muPart.Unlock()

		var buf bytes.Buffer
		for {
			_, err := io.CopyN(&buf, as.source, int64(as.bufferChunk))
			if err != nil {
				if err != io.EOF {
					log.Fatalf("while copying audio input to streamer buffer: %v", err)
				}
			}
			if time.Now().After(stopTime) {
				break
			}
		}
		as.muPart.Lock()
		as.currentPart = buf.Bytes()
		if flagDebug {
			// write the wave file asynchronously, so we can go back to receiving audio ASAP
			var thecopy []byte = make([]byte, len(as.currentPart))
			copy(thecopy, as.currentPart)
			go func(data []byte) {
				// this is all self-racy, but whatever
				if wavCount > 10 {
					wavCount = 1
				}
				wavefile := filepath.Join(tempDir, fmt.Sprintf("debug%d.wav", wavCount))
				if err := writeWav(wavefile, data); err != nil {
					log.Fatal(err)
				}
				wavCount++
			}(thecopy)
		}
		as.muPart.Unlock()
	}
}

// for debugging.
func writeWav(filename string, data []byte) error {
	rd := bytes.NewReader(data)
	var asInts []int
	for {
		i, err := binary.ReadVarint(rd)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				return err
			}
		}
		asInts = append(asInts, int(i))
		// EOF
		if err != nil {
			break
		}
	}

	buf := &audio.IntBuffer{
		// Format is the representation of the underlying data format
		Format: &audio.Format{
			NumChannels: numChannels,
			SampleRate:  rate,
		},
		Data:           asInts,
		SourceBitDepth: bitDepth,
	}

	out, err := os.Create(filename)
	if err != nil {
		return err
	}

	e := wav.NewEncoder(out,
		buf.Format.SampleRate,
		int(buf.SourceBitDepth),
		buf.Format.NumChannels,
		1)
	if err := e.Write(buf); err != nil {
		return err
	}
	if err = e.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func handleCLISignals() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		l := sc.Text()
		if l != "toggle" {
			continue
		}
		if err := toggleStartStop(); err != nil {
			log.Println(err)
		}
	}
}

var noCoreURLErr = errors.New("core URL not provided")

func handleSignal(recording bool) error {
	if flagCoreURL == "" {
		return noCoreURLErr
	}
	theurl := flagCoreURL
	if recording {
		if flagVerbose {
			log.Printf("Sending start signal")
		}
		theurl += "/start"
	} else {
		if flagVerbose {
			log.Printf("Sending stop signal")
		}
		theurl += "/stop"
	}
	res, err := http.Get(theurl)
	if err != nil {
		return err
	}
	code := res.StatusCode
	if code != 200 {
		return fmt.Errorf("unexpected code in response from core: %d", code)
	}
	res.Body.Close()
	return nil
}

func toggleStartStop() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	// TODO: maybe merge startRecording and stopRecording together again if it makes sense.
	if isRecording {
		return stopRecording()
	}
	return startRecording()
}

func stopRecordingLocked() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return stopRecording()
}

// needs to be locked by caller
func stopRecording() error {
	// in case we're in a manual stop, cancel the programmed autostop
	// that was supposed to happen eventually.
	if autostop != nil {
		autostop.Stop()
		autostop = nil
	}

	if !isRecording {
		// already stopped, all good
		return nil
	}

	isRecording = !isRecording

	if err := setLED(false, isErroring); err != nil {
		return err
	}

	go handleSignalRetry(isRecording)

	return nil
}

func startRecordingLocked() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return startRecording()
}

// needs to be locked by caller
func startRecording() error {
	if isRecording {
		// already started, all good
		return nil
	}

	isRecording = !isRecording
	autostop = time.AfterFunc(time.Duration(flagMaxAudio)*time.Second, func() {
		if err := stopRecordingLocked(); err != nil {
			log.Printf("while running automatic stop recording: %v", err)
		}
	})

	if err := setLED(true, isErroring); err != nil {
		return err
	}

	go handleSignalRetry(isRecording)

	return nil
}

// routinely retry signalling recording status to core, with exponential backoff.
// this func NEEDS to be in a goroutine, as it locks the mutex.
func handleSignalRetry(recording bool) {
	pause := time.Second
	maxPause := 5 * time.Minute
	start := time.Now()
	maxRetry := time.Duration(flagMaxAudio) * time.Second * 9 / 10
	for {
		stateMu.RLock()
		rec := isRecording
		stateMu.RUnlock()
		if rec != recording {
			// status has changed in the meantime, so we stop trying
			return
		}
		if recording {
			if time.Since(start) > maxRetry {
				// we're about to stop recording anyway, so no point anymore in
				// telling the core to start fetching audio
				return
			}
		}
		err := handleSignal(recording)
		if err == nil {
			return
		}
		if err == noCoreURLErr {
			return
		}
		if flagVerbose {
			log.Printf("failed to signal recording status (%v) to core: %v", recording, err)
		}
		time.Sleep(pause)
		pause *= 2
		if pause > maxPause {
			pause = maxPause
		}
	}
}

type chip interface {
	NewLine(int) (line, error)
}

type line interface {
	SetValue(int) error
	Close() error
}
