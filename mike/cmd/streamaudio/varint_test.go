// Copyright (c) The Arribada initiative.
// Licensed under the GPLv3 License.

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"testing"
)

func TestVarint(t *testing.T) {
	pi := []int{3, 14, 15, 92, 65, 35, 89, 793, 238, 46}

	var asBytes []byte
	for _, v := range pi {
		asBytes = binary.AppendVarint(asBytes, int64(v))
	}

	t.Log("LOSING BYTES")
	for i := 0; i < len(asBytes); i++ {
		fmt.Println(pi)
		loseByte(asBytes, i)
		fmt.Println()
	}

	t.Log("ADDING BYTES")
	for i := 0; i < len(asBytes); i++ {
		fmt.Println(pi)
		gainByte(asBytes, i)
		fmt.Println()
	}

	t.Log("CHANGING BYTES")
	for i := 0; i < len(asBytes); i++ {
		fmt.Println(pi)
		changeByte(asBytes, i)
		fmt.Println()
	}
}

func loseByte(asBytes []byte, pos int) {
	seq := make([]byte, len(asBytes))
	copy(seq, asBytes)
	asBytes2 := append(seq[0:pos], seq[pos+1:]...)

	var pi2 []int
	rd := bytes.NewReader(asBytes2)
	for {
		i, err := binary.ReadVarint(rd)
		if err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}
		}
		pi2 = append(pi2, int(i))
		// End of current chunk of data
		if err != nil {
			break
		}
	}
	fmt.Println(pi2)
}

func gainByte(asBytes []byte, pos int) {
	seq := make([]byte, len(asBytes)+1)
	copy(seq, asBytes)
	asBytes2 := append(seq[0:pos], 7)
	asBytes2 = append(asBytes2, asBytes[pos:]...)

	var pi2 []int
	rd := bytes.NewReader(asBytes2)
	for {
		i, err := binary.ReadVarint(rd)
		if err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}
		}
		pi2 = append(pi2, int(i))
		// End of current chunk of data
		if err != nil {
			break
		}
	}
	fmt.Println(pi2)
}

func changeByte(asBytes []byte, pos int) {
	asBytes2 := make([]byte, len(asBytes))
	copy(asBytes2, asBytes)
	asBytes2[pos] = 7

	var pi2 []int
	rd := bytes.NewReader(asBytes2)
	for {
		i, err := binary.ReadVarint(rd)
		if err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}
		}
		pi2 = append(pi2, int(i))
		// End of current chunk of data
		if err != nil {
			break
		}
	}
	fmt.Println(pi2)
}
