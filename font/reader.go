package font

import (
	"encoding/binary"
	"fmt"
)

// reader is a cursor over a byte slice that never panics: any out-of-range
// access latches err and returns zero. Callers check err once at the end.
type reader struct {
	b   []byte
	off int
	err error
}

func newReader(b []byte) *reader { return &reader{b: b} }

// at returns a sub-reader positioned at absolute offset off.
func (r *reader) at(off int) *reader {
	if r.err != nil {
		return &reader{err: r.err}
	}
	if off < 0 || off > len(r.b) {
		return &reader{err: fmt.Errorf("%w: offset %d out of range (len %d)", ErrMalformed, off, len(r.b))}
	}
	return &reader{b: r.b, off: off}
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.off+n > len(r.b) || r.off+n < r.off {
		r.err = fmt.Errorf("%w: read of %d bytes at offset %d exceeds length %d", ErrMalformed, n, r.off, len(r.b))
		return false
	}
	return true
}

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}

func (r *reader) i16() int16 { return int16(r.u16()) }

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *reader) tag() string {
	if !r.need(4) {
		return ""
	}
	s := string(r.b[r.off : r.off+4])
	r.off += 4
	return s
}

// slice returns n bytes at the current offset without copying.
func (r *reader) slice(n int) []byte {
	if !r.need(n) {
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

// fixed reads a 16.16 fixed-point number as float64.
func (r *reader) fixed() float64 {
	v := r.u32()
	return float64(int32(v)) / 65536.0
}
