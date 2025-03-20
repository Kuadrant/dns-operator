package kuadrant

import (
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Logic taken from https://github.com/coredns/coredns/blob/master/plugin/loadbalance/weighted.go

type weightedRR struct {
	dns.RR
	weight int64
}

type weightedRRSet struct {
	rrs []weightedRR
	randomGen
	mutex sync.Mutex
}

func newWeightedRRSet(rrs []weightedRR) *weightedRRSet {
	wRRSet := &weightedRRSet{
		rrs:       rrs,
		randomGen: &randomUint{},
	}
	wRRSet.randomGen.randInit()
	return wRRSet
}

// Random uint generator
type randomGen interface {
	randInit()
	randUint(limit uint) uint
}

// Random uint generator
type randomUint struct {
	rn *rand.Rand
}

func (r *randomUint) randInit() {
	r.rn = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func (r *randomUint) randUint(limit uint) uint {
	return uint(r.rn.Intn(int(limit)))
}

// Move the next expected address to the first position in the result list
func (w *weightedRRSet) setTopRecord(address []weightedRR) {
	itop := w.topAddressIndex(address)

	if itop < 0 {
		// internal error
		return
	}

	if itop != 0 {
		// swap the selected top entry with the actual one
		address[0], address[itop] = address[itop], address[0]
	}
}

// Compute the top (first) address index
func (w *weightedRRSet) topAddressIndex(address []weightedRR) int {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// Determine the weight value for each address in the answer
	var wsum uint

	for _, r := range address {
		wsum += uint(r.weight)
	}

	// Select the first (top) IP
	sort.Slice(address, func(i, j int) bool {
		return address[i].weight > address[j].weight
	})
	v := w.randUint(wsum)
	var psum uint
	for index, wa := range address {
		psum += uint(wa.weight)
		if v < psum {
			log.Debugf("returning %v", index)
			return index
		}
	}

	// we should never reach this
	log.Errorf("Internal error: cannot find top address (randv:%v wsum:%v)", v, wsum)
	return -1
}

func (w *weightedRRSet) getTopRR() *dns.RR {
	w.setTopRecord(w.rrs)
	return &w.rrs[0].RR
}
