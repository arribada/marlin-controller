// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"flag"
	"log"
	"os/exec"
	"time"
	// "github.com/go-audio/wav"
	// "github.com/gordonklaus/portaudio"
)

var (
	flagWavFile string
	flagPause   int
)

func init() {
	flag.StringVar(&flagWavFile, "wavfile", "./wav/jfk.wav", "use the given wave file as input for sox.")
	flag.IntVar(&flagPause, "pause", 1, "how many seconds to pause in between plays")
}

func main() {
	flag.Parse()

	play()
}

func play() {
	/*
		if err := portaudio.Initialize(); err != nil {
			log.Fatalf("initializing portaudio: %v", err)
		}
		defer func() {
			_ = portaudio.Terminate()
		}()
	*/

	for {
		fromSox()
		// fromPA()
		time.Sleep(time.Duration(flagPause) * time.Second)
	}
}

func fromSox() {
	cmd := exec.Command("play", flagWavFile)
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

/*
var audio []int16

const chunkSize = 8192

func fromPA() {
	if audio == nil {
		f, err := os.Open(flagWavFile)
		if err != nil {
			log.Fatal(err)
		}

		d := wav.NewDecoder(f)
		asInts, err := d.FullPCMBuffer()
		if err != nil {
			log.Fatal(err)
		}
		audio = make([]int16, len(asInts.Data))
		for k, v := range asInts.Data {
			audio[k] = int16(v)
		}
	}

	out := make([]int16, chunkSize)
	stream, err := portaudio.OpenDefaultStream(0, 1, 16000, len(out), &out)
	if err != nil {
		log.Fatalf("opening stream: %v", err)
	}
	defer func() {
		stream.Close()
	}()

	if err := stream.Start(); err != nil {
		log.Fatalf("starting stream: %v", err)
	}
	defer func() {
		_ = stream.Stop()
	}()

	offset := 0
	upper := chunkSize
	for {
		if upper > len(audio) {
			upper = len(audio)
		}
		copy(out, audio[offset:upper])
		if err := stream.Write(); err != nil {
			log.Fatalf("writing stream: %v", err)
		}
		if upper == len(audio) {
			break
		}
		offset = upper
		upper += chunkSize
	}
}
*/
