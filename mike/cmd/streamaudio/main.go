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
	flagHost    string
	flagCoreURL string
	flagVerbose bool
	flagDebug   bool
)

func init() {
	flag.StringVar(&flagHost, "host", "localhost:8080", "host:port on which the audio is served")
	flag.BoolVar(&flagVerbose, "v", false, "be verbose")
	flag.BoolVar(&flagDebug, "debug", false, "enable debugging helpers. unrelated to logging.")
	flag.StringVar(&flagCoreURL, "core", "http://localhost:8080", "where to forward the start/stop signals to, to stop the audio transcribing")
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
	bufferSize = 20
	// arbitrary.
	bufferChunk = 4096
)

var (
	ledStateMu  sync.Mutex
	isRecording bool
	isErroring  bool
)

var coreURL string

func main() {
	flag.Parse()

	/*
		var err error
			coreURL, err = common.MResolve(flagCoreURL)
			if err != nil {
				log.Fatal(err)
			}
	*/
	coreURL = flagCoreURL

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

	stream()
}

func stream() {
	pr, pw := io.Pipe()
	bs := time.Duration(bufferSize) * time.Second
	as := &audioStreamer{
		source:      pr,
		bufferSize:  bs,
		bufferChunk: bufferChunk,
		nextPart:    time.Now().Add(bs),
	}
	go func() {
		if err := fromMic(pw); err != nil {
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

// TODO: on mac, record with sox instead
func fromMic(output io.Writer) error {
	rec, err := device()
	if err != nil {
		return err
	}

	if err = rec.Open(); err != nil {
		return err
	}
	defer rec.Close()

	if err := prepare(rec); err != nil {
		return err
	}

	// chan size does not really matter here, as long as it's buffered/async.
	workC := make(chan []byte, 100)

	// Asynchronous reception of the audio capture, and then piping it to
	// our audio streamer. We have it async here, so that the main loop on the
	// audio capture can spend most of its time on the audio capture itself,
	// without too much interuption.
	go func() {
		if err := processAudio(context.TODO(), output, workC); err != nil {
			log.Fatal(err)
		}
	}()

	recordAudio(context.TODO(), rec, workC)
	return errors.New("recording unexpectedly stopped")
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
		if l != "start" && l != "stop" {
			continue
		}
		if l == "start" {
			if err := handleSignal(true); err != nil {
				log.Println(err)
				continue
			}
			if err := setLED(true, false); err != nil {
				log.Println(err)
				continue
			}
			continue
		}

		if err := handleSignal(false); err != nil {
			log.Println(err)
			continue
		}
		if err := setLED(false, false); err != nil {
			log.Println(err)
			continue
		}
	}
}

func handleSignal(start bool) error {
	theurl := coreURL
	if start {
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

func toggleStartStop() {
	ledStateMu.Lock()
	defer ledStateMu.Unlock()

	if err := handleSignal(!isRecording); err != nil {
		log.Println(err)
		return
	}
	isRecording = !isRecording
	if err := setLED(isRecording, isErroring); err != nil {
		log.Println(err)
	}
}

type chip interface {
	NewLine(int) (line, error)
}

type line interface {
	SetValue(int) error
	Close() error
}
