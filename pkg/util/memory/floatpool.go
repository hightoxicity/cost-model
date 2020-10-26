package memory

import (
	"container/list"
	"fmt"
	"sync"
	"unsafe"
)

// FloatPool is a float64 buffer capable of leasing out slices for temporary use.
// This can reduce total heap allocations for critcial code paths. This type is
// thread safe.
type FloatPool struct {
	buf         []*float64
	allocations AllocationList
	pos         int
	start       *list.Element
	lock        *sync.Mutex
}

// Create a new float pool with a default size buffer. The buffer will double size
// each time it's required to grow.
func NewFloatPool(size int) *FloatPool {
	return &FloatPool{
		buf:         make([]*float64, size),
		pos:         0,
		start:       nil,
		allocations: NewAllocationList(),
		lock:        new(sync.Mutex),
	}
}

// Make creates a new slice allocation from the pool and returns it.
// Any slices created by the pool should be explicitly returned to
// the pool once it is no longer used. Ensure any data that must persist
// is copied before returned. Failure to return a slice can result in
// leaks and unnecessary pooled allocations.
func (fp *FloatPool) Make(length int) []*float64 {
	fp.lock.Lock()
	defer fp.lock.Unlock()

	// find the next allocation location, resize buffer if necessary
	next, ele := fp.allocations.Next(fp.start, fp.pos, len(fp.buf), length)

	// if the next allocation location + length is larger than the buffer,
	// grow the buffer
	buffLength := len(fp.buf)
	if next+length >= buffLength {
		for next+length >= buffLength {
			buffLength = buffLength * 2
		}

		newBuf := make([]*float64, buffLength)
		copy(newBuf, fp.buf)
		fp.buf = newBuf
	}

	// create the slice from subset of buf
	sl := fp.buf[next : next+length]

	// insert allocation record, advance search position
	ele = fp.allocations.InsertBefore(&Allocation{
		Offset: next,
		Size:   length,
		Addr:   fp.addressFor(sl),
	}, ele)

	fp.pos = next + length + 1
	fp.start = ele

	return sl
}

// Return accepts a slice allocation that was created by calling Make on
// this pool instance. Ensure any data that must persist from the returned
// slice is copied. Failure to return a slice can result in leaks and
// unnecessary additional pooled allocations.
func (fp *FloatPool) Return(v []*float64) {
	fp.lock.Lock()
	defer fp.lock.Unlock()

	removed, next := fp.allocations.Remove(fp.addressFor(v))
	if removed == nil {
		fmt.Printf("Error: Failed to locate allocated slice\n")
		return
	}

	// set the search start at the lowest returned
	if removed.Offset < fp.pos {
		fp.pos = removed.Offset
		fp.start = next
	}

	// nil out returned slice
	fp.clear(v)
}

// nils out indices of the slice parameter
func (fp *FloatPool) clear(v []*float64) {
	for i := range v {
		v[i] = nil
	}
}

// addressFor finds the address for the slice
func (fp *FloatPool) addressFor(v []*float64) uintptr {
	return uintptr(unsafe.Pointer(&v[0]))
}