// Package sfntwrite assembles sfnt (TTF) files from raw tables with correct
// checksums. Output is deterministic: tables are written in sorted tag
// order with zero padding.
package sfntwrite

import (
	"encoding/binary"
	"sort"
)

// Assemble packs tables into a complete sfnt file. The head table, if
// present, gets a correct checkSumAdjustment.
func Assemble(tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	n := len(tags)
	headerLen := 12 + 16*n
	out := make([]byte, headerLen)
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	// Compute a valid searchRange etc. (largest power of two <= n).
	maxPow2 := 1
	entrySelector := 0
	for maxPow2*2 <= n {
		maxPow2 *= 2
		entrySelector++
	}
	binary.BigEndian.PutUint16(out[6:], uint16(maxPow2*16))
	binary.BigEndian.PutUint16(out[8:], uint16(entrySelector))
	binary.BigEndian.PutUint16(out[10:], uint16(n*16-maxPow2*16))
	type rec struct {
		tag  string
		off  int
		data []byte
	}
	var recs []rec
	off := headerLen
	for _, t := range tags {
		d := tables[t]
		recs = append(recs, rec{t, off, d})
		off += len(d)
		for off%4 != 0 {
			off++
		}
	}
	out = append(out, make([]byte, off-headerLen)...)
	for i, rc := range recs {
		entry := out[12+16*i:]
		copy(entry[0:4], rc.tag)
		binary.BigEndian.PutUint32(entry[8:], uint32(rc.off))
		binary.BigEndian.PutUint32(entry[12:], uint32(len(rc.data)))
		copy(out[rc.off:], rc.data)
		var sum uint32
		padded := out[rc.off:]
		end := (len(rc.data) + 3) &^ 3
		for j := 0; j+4 <= end; j += 4 {
			sum += binary.BigEndian.Uint32(padded[j:])
		}
		binary.BigEndian.PutUint32(entry[4:], sum)
	}
	// checkSumAdjustment in head.
	var headOff = -1
	for _, rc := range recs {
		if rc.tag == "head" {
			headOff = rc.off
		}
	}
	if headOff >= 0 {
		binary.BigEndian.PutUint32(out[headOff+8:], 0)
		var total uint32
		for j := 0; j+4 <= len(out); j += 4 {
			total += binary.BigEndian.Uint32(out[j:])
		}
		binary.BigEndian.PutUint32(out[headOff+8:], 0xB1B0AFBA-total)
	}
	return out
}

// Checksum computes the sfnt table checksum of b (zero-padded to 4 bytes).
func Checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var v uint32
		for j := 0; j < 4 && i+j < len(b); j++ {
			v |= uint32(b[i+j]) << uint(24-8*j)
		}
		sum += v
	}
	return sum
}
