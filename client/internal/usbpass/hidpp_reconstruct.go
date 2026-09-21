package usbpass

// Reconstruction of a HID Report Descriptor from a Windows HIDP_PREPARSED_DATA
// blob.
//
// Windows never hands a user-mode process the raw report descriptor, only the
// parsed form (HidD_GetPreparsedData). The blob layout is undocumented; it was
// reverse engineered by the hidapi project, and this file is a Go port of
// hidapi's windows/hidapi_descriptor_reconstruct.c (libusb/hidapi, offered
// under the BSD-style license, (c) libusb/hidapi Team). The result is a
// functionally equivalent descriptor: same collections, usages, report IDs and
// bit layout, but not necessarily byte-identical to what the device sends
// (padding items, item ordering of globals).
//
// Deliberate limitation: aliased usages/collections (report descriptor
// "delimiter" items) are not supported and produce an error, so such a device
// keeps using the libusb path instead of being exported with a wrong
// descriptor.
//
// Pure Go on purpose (no Windows API): the blob parsing and the reconstruction
// are unit-testable anywhere.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	ppHeaderSize = 44
	ppCapSize    = 104
	ppLinkSize   = 16

	ppReportTypes = 3 // input, output, feature (HIDP_REPORT_TYPE order)
)

// ppCap mirrors hid_pp_cap (104 bytes).
type ppCap struct {
	UsagePage      uint16
	ReportID       uint8
	BitPosition    uint8
	ReportSize     uint16
	ReportCount    uint16
	BytePosition   uint16
	BitCount       uint16
	BitField       uint32
	LinkCollection uint16
	LinkUsagePage  uint16
	LinkUsage      uint16

	IsButtonCap       bool
	IsRange           bool
	IsAlias           bool
	IsStringRange     bool
	IsDesignatorRange bool

	// Range / NotRange union (same offsets: Usage == UsageMin, and so on).
	UsageMin, UsageMax           uint16
	StringMin, StringMax         uint16
	DesignatorMin, DesignatorMax uint16
	DataIndexMin, DataIndexMax   uint16

	// Button caps carry only a logical range; everything else the full set.
	ButtonLogicalMin, ButtonLogicalMax int32
	LogicalMin, LogicalMax             int32
	PhysicalMin, PhysicalMax           int32
	Units, UnitsExp                    uint32
}

type ppLink struct {
	LinkUsage        uint16
	LinkUsagePage    uint16
	Parent           uint16
	NumberOfChildren uint16
	NextSibling      uint16
	FirstChild       uint16
	CollectionType   uint8
	IsAlias          bool
}

type ppCapsInfo struct {
	FirstCap         uint16
	NumberOfCaps     uint16
	LastCap          uint16
	ReportByteLength uint16
}

type ppData struct {
	Usage, UsagePage uint16
	Info             [ppReportTypes]ppCapsInfo
	Caps             []ppCap
	Links            []ppLink
}

// preparsedBlobSize returns how many bytes the blob at the given header
// occupies, so a caller holding only a pointer to it can copy exactly that.
func preparsedBlobSize(header []byte) (int, error) {
	if len(header) < ppHeaderSize {
		return 0, errors.New("preparsed data: short header")
	}
	if !bytes.Equal(header[:8], []byte("HidP KDR")) {
		return 0, errors.New("preparsed data: unknown magic")
	}
	linkOff := int(binary.LittleEndian.Uint16(header[40:42]))
	nLinks := int(binary.LittleEndian.Uint16(header[42:44]))
	return ppHeaderSize + linkOff + nLinks*ppLinkSize, nil
}

func parsePreparsed(b []byte) (*ppData, error) {
	total, err := preparsedBlobSize(b)
	if err != nil {
		return nil, err
	}
	if len(b) < total {
		return nil, fmt.Errorf("preparsed data: truncated (%d < %d)", len(b), total)
	}
	le := binary.LittleEndian
	pp := &ppData{Usage: le.Uint16(b[8:]), UsagePage: le.Uint16(b[10:])}
	for i := 0; i < ppReportTypes; i++ {
		o := 16 + i*8
		pp.Info[i] = ppCapsInfo{le.Uint16(b[o:]), le.Uint16(b[o+2:]), le.Uint16(b[o+4:]), le.Uint16(b[o+6:])}
	}
	linkOff := int(le.Uint16(b[40:]))
	nLinks := int(le.Uint16(b[42:]))
	nCaps := linkOff / ppCapSize
	pp.Caps = make([]ppCap, nCaps)
	for i := range pp.Caps {
		c := b[ppHeaderSize+i*ppCapSize : ppHeaderSize+(i+1)*ppCapSize]
		flags := c[24]
		pp.Caps[i] = ppCap{
			UsagePage:         le.Uint16(c[0:]),
			ReportID:          c[2],
			BitPosition:       c[3],
			ReportSize:        le.Uint16(c[4:]),
			ReportCount:       le.Uint16(c[6:]),
			BytePosition:      le.Uint16(c[8:]),
			BitCount:          le.Uint16(c[10:]),
			BitField:          le.Uint32(c[12:]),
			LinkCollection:    le.Uint16(c[18:]),
			LinkUsagePage:     le.Uint16(c[20:]),
			LinkUsage:         le.Uint16(c[22:]),
			IsButtonCap:       flags&(1<<2) != 0,
			IsRange:           flags&(1<<4) != 0,
			IsAlias:           flags&(1<<5) != 0,
			IsStringRange:     flags&(1<<6) != 0,
			IsDesignatorRange: flags&(1<<7) != 0,
			UsageMin:          le.Uint16(c[60:]),
			UsageMax:          le.Uint16(c[62:]),
			StringMin:         le.Uint16(c[64:]),
			StringMax:         le.Uint16(c[66:]),
			DesignatorMin:     le.Uint16(c[68:]),
			DesignatorMax:     le.Uint16(c[70:]),
			DataIndexMin:      le.Uint16(c[72:]),
			DataIndexMax:      le.Uint16(c[74:]),
			ButtonLogicalMin:  int32(le.Uint32(c[76:])),
			ButtonLogicalMax:  int32(le.Uint32(c[80:])),
			LogicalMin:        int32(le.Uint32(c[80:])),
			LogicalMax:        int32(le.Uint32(c[84:])),
			PhysicalMin:       int32(le.Uint32(c[88:])),
			PhysicalMax:       int32(le.Uint32(c[92:])),
			Units:             le.Uint32(c[96:]),
			UnitsExp:          le.Uint32(c[100:]),
		}
	}
	pp.Links = make([]ppLink, nLinks)
	for i := range pp.Links {
		o := ppHeaderSize + linkOff + i*ppLinkSize
		l := b[o : o+ppLinkSize]
		bits := le.Uint32(l[12:])
		pp.Links[i] = ppLink{
			LinkUsage:        le.Uint16(l[0:]),
			LinkUsagePage:    le.Uint16(l[2:]),
			Parent:           le.Uint16(l[4:]),
			NumberOfChildren: le.Uint16(l[6:]),
			NextSibling:      le.Uint16(l[8:]),
			FirstChild:       le.Uint16(l[10:]),
			CollectionType:   uint8(bits & 0xFF),
			IsAlias:          bits&(1<<8) != 0,
		}
	}
	return pp, nil
}

// ---- main-item node list (a singly linked list, as in the reference) ----

type rdMain int

const (
	rdInput rdMain = iota
	rdOutput
	rdFeature
	rdCollection
	rdCollectionEnd
)

type rdNode struct {
	firstBit, lastBit int
	padding           bool
	capsIndex         int
	collIndex         int
	main              rdMain
	reportID          uint8
	next              *rdNode
}

func rdAppend(head **rdNode, n rdNode) *rdNode {
	nn := n
	nn.next = nil
	for *head != nil {
		head = &(*head).next
	}
	*head = &nn
	return &nn
}

// rdInsertAfter inserts n after node at and returns the new node.
func rdInsertAfter(at *rdNode, n rdNode) *rdNode {
	nn := n
	nn.next = at.next
	at.next = &nn
	return &nn
}

// rdSearch returns the last node before the first INPUT/OUTPUT/FEATURE item of
// the given type/report whose last bit reaches searchBit (or before the next
// collection boundary), starting from start.
func rdSearch(start *rdNode, searchBit int, typ rdMain, reportID uint8) *rdNode {
	l := start
	for l.next.main != rdCollection && l.next.main != rdCollectionEnd &&
		!(l.next.lastBit >= searchBit && l.next.reportID == reportID && l.next.main == typ) {
		l = l.next
	}
	return l
}

type bitRange struct{ first, last int }

// Report-descriptor item prefixes.
const (
	rdItemInput       = 0x80
	rdItemOutput      = 0x90
	rdItemFeature     = 0xB0
	rdItemCollection  = 0xA0
	rdItemEndColl     = 0xC0
	rdItemUsagePage   = 0x04
	rdItemLogicalMin  = 0x14
	rdItemLogicalMax  = 0x24
	rdItemPhysicalMin = 0x34
	rdItemPhysicalMax = 0x44
	rdItemUnitExp     = 0x54
	rdItemUnit        = 0x64
	rdItemReportSize  = 0x74
	rdItemReportID    = 0x84
	rdItemReportCount = 0x94
	rdItemUsage       = 0x08
	rdItemUsageMin    = 0x18
	rdItemUsageMax    = 0x28
	rdItemDesigIndex  = 0x38
	rdItemDesigMin    = 0x48
	rdItemDesigMax    = 0x58
	rdItemString      = 0x78
	rdItemStringMin   = 0x88
	rdItemStringMax   = 0x98
)

func rdWriteItem(out *[]byte, item byte, data int64) error {
	switch item {
	case rdItemEndColl:
		*out = append(*out, item)
		return nil
	case rdItemLogicalMin, rdItemLogicalMax, rdItemPhysicalMin, rdItemPhysicalMax:
		switch {
		case data >= -128 && data <= 127:
			*out = append(*out, item|1, byte(int8(data)))
		case data >= -32768 && data <= 32767:
			*out = append(*out, item|2, byte(data), byte(data>>8))
		case data >= -2147483648 && data <= 2147483647:
			*out = append(*out, item|3, byte(data), byte(data>>8), byte(data>>16), byte(data>>24))
		default:
			return errors.New("descriptor item out of range")
		}
		return nil
	}
	switch {
	case data >= 0 && data <= 0xFF:
		*out = append(*out, item|1, byte(data))
	case data >= 0 && data <= 0xFFFF:
		*out = append(*out, item|2, byte(data), byte(data>>8))
	case data >= 0 && data <= 0xFFFFFFFF:
		*out = append(*out, item|3, byte(data), byte(data>>8), byte(data>>16), byte(data>>24))
	default:
		return errors.New("descriptor item out of range")
	}
	return nil
}

// reconstructReportDescriptor rebuilds the report descriptor of the single top
// level collection a preparsed-data blob describes (Windows keeps one blob per
// top-level collection, i.e. per HID "COLnn" device).
func reconstructReportDescriptor(pp *ppData) (desc []byte, err error) {
	// The blob layout is reverse engineered; a truncated or unexpected one must
	// end up as an error (device stays on the libusb path), not a crash.
	defer func() {
		if r := recover(); r != nil {
			desc, err = nil, fmt.Errorf("preparsed data: malformed (%v)", r)
		}
	}()
	nLinks := len(pp.Links)
	if nLinks == 0 {
		return nil, errors.New("preparsed data has no collections")
	}
	for i := range pp.Links {
		if pp.Links[i].IsAlias {
			return nil, errors.New("aliased collections (delimiters) are not supported")
		}
	}
	for rt := 0; rt < ppReportTypes; rt++ {
		info := pp.Info[rt]
		if int(info.LastCap) > len(pp.Caps) {
			return nil, errors.New("preparsed data: caps index out of range")
		}
		for ci := int(info.FirstCap); ci < int(info.LastCap); ci++ {
			c := &pp.Caps[ci]
			if c.IsAlias {
				return nil, errors.New("aliased usages (delimiters) are not supported")
			}
			if int(c.LinkCollection) >= nLinks {
				return nil, errors.New("preparsed data: bad link collection index")
			}
		}
	}

	// coll_bit_range[collection][reportID][reportType]
	collBits := make([][256][ppReportTypes]bitRange, nLinks)
	for i := range collBits {
		for r := 0; r < 256; r++ {
			for t := 0; t < ppReportTypes; t++ {
				collBits[i][r][t] = bitRange{-1, -1}
			}
		}
	}
	capBits := func(c *ppCap) (int, int) {
		first := (int(c.BytePosition)-1)*8 + int(c.BitPosition)
		return first, first + int(c.ReportSize)*int(c.ReportCount) - 1
	}
	for rt := 0; rt < ppReportTypes; rt++ {
		for ci := int(pp.Info[rt].FirstCap); ci < int(pp.Info[rt].LastCap); ci++ {
			c := &pp.Caps[ci]
			first, last := capBits(c)
			br := &collBits[c.LinkCollection][c.ReportID][rt]
			if br.first == -1 || br.first > first {
				br.first = first
			}
			if br.last < last {
				br.last = last
			}
		}
	}

	// Hierarchy levels and direct-child counts.
	levels := make([]int, nLinks)
	directChilds := make([]int, nLinks)
	for i := range levels {
		levels[i] = -1
	}
	maxLevel := 0
	{
		actual, idx := 0, 0
		for guard := 0; actual >= 0; guard++ {
			if guard > 8*nLinks+16 {
				return nil, errors.New("preparsed data: collection tree loop")
			}
			levels[idx] = actual
			l := &pp.Links[idx]
			if l.NumberOfChildren > 0 && int(l.FirstChild) < nLinks && levels[l.FirstChild] == -1 {
				actual++
				levels[idx] = actual
				if maxLevel < actual {
					maxLevel = actual
				}
				directChilds[idx]++
				idx = int(l.FirstChild)
			} else if l.NextSibling != 0 && int(l.NextSibling) < nLinks {
				directChilds[l.Parent]++
				idx = int(l.NextSibling)
			} else {
				actual--
				if actual >= 0 {
					idx = int(l.Parent)
				}
			}
		}
	}

	// Propagate child bit ranges to the parents. The reference advances the
	// sibling cursor inside its innermost loop; kept as is for identical output.
	for level := maxLevel - 1; level >= 0; level-- {
		for idx := 0; idx < nLinks; idx++ {
			if levels[idx] != level {
				continue
			}
			child := int(pp.Links[idx].FirstChild)
			for guard := 0; child != 0 && child < nLinks; guard++ {
				if guard > 4096 {
					return nil, errors.New("preparsed data: sibling loop")
				}
				for r := 0; r < 256 && child != 0 && child < nLinks; r++ {
					for t := 0; t < ppReportTypes && child < nLinks; t++ {
						cb := collBits[child][r][t]
						pb := &collBits[idx][r][t]
						if cb.first != -1 && pb.first > cb.first {
							pb.first = cb.first
						}
						if pb.last < cb.last {
							pb.last = cb.last
						}
						child = int(pp.Links[child].NextSibling)
					}
				}
			}
		}
	}

	// Child collection order, sorted by bit position.
	childOrder := make([][]uint16, nLinks)
	{
		parsed := make([]bool, nLinks)
		actual, idx := 0, 0
		for guard := 0; actual >= 0; guard++ {
			if guard > 8*nLinks+16 {
				return nil, errors.New("preparsed data: collection order loop")
			}
			l := &pp.Links[idx]
			if directChilds[idx] != 0 && !parsed[l.FirstChild] {
				parsed[l.FirstChild] = true
				n := directChilds[idx]
				order := make([]uint16, n)
				child := l.FirstChild
				cnt := n - 1
				order[cnt] = child
				for pp.Links[child].NextSibling != 0 && cnt > 0 {
					cnt--
					child = pp.Links[child].NextSibling
					order[cnt] = child
				}
				childOrder[idx] = order
				if n > 1 {
					for rt := 0; rt < ppReportTypes; rt++ {
						for r := 0; r < 256; r++ {
							for c := 1; c < n; c++ {
								prev, cur := order[c-1], order[c]
								if collBits[prev][r][rt].first != -1 && collBits[cur][r][rt].first != -1 &&
									collBits[prev][r][rt].first > collBits[cur][r][rt].first {
									order[c-1], order[c] = order[c], order[c-1]
								}
							}
						}
					}
				}
				actual++
				idx = int(l.FirstChild)
			} else if l.NextSibling != 0 {
				idx = int(l.NextSibling)
			} else {
				actual--
				if actual >= 0 {
					idx = int(l.Parent)
				}
			}
		}
	}

	// Collection / End Collection skeleton.
	var head *rdNode
	collBegin := make([]*rdNode, nLinks)
	collEnd := make([]*rdNode, nLinks)
	{
		lastChild := make([]int, nLinks)
		for i := range lastChild {
			lastChild[i] = -1
		}
		actual, idx := 0, 0
		collBegin[0] = rdAppend(&head, rdNode{main: rdCollection, collIndex: 0})
		for guard := 0; actual >= 0; guard++ {
			if guard > 16*nLinks+16 {
				return nil, errors.New("preparsed data: collection walk loop")
			}
			if directChilds[idx] != 0 && lastChild[idx] == -1 {
				lastChild[idx] = int(childOrder[idx][0])
				idx = int(childOrder[idx][0])
				collBegin[idx] = rdAppend(&head, rdNode{main: rdCollection, collIndex: idx})
				actual++
			} else if directChilds[idx] > 1 && lastChild[idx] != int(childOrder[idx][directChilds[idx]-1]) {
				next := 1
				for lastChild[idx] != int(childOrder[idx][next-1]) {
					next++
				}
				lastChild[idx] = int(childOrder[idx][next])
				idx = int(childOrder[idx][next])
				collBegin[idx] = rdAppend(&head, rdNode{main: rdCollection, collIndex: idx})
				actual++
			} else {
				actual--
				collEnd[idx] = rdAppend(&head, rdNode{main: rdCollectionEnd, collIndex: idx})
				idx = int(pp.Links[idx].Parent)
			}
		}
	}
	for i := 0; i < nLinks; i++ {
		if collBegin[i] == nil || collEnd[i] == nil {
			return nil, errors.New("preparsed data: unreachable collection")
		}
	}

	// Insert the Input/Output/Feature main items at their bit positions.
	for rt := 0; rt < ppReportTypes; rt++ {
		for ci := int(pp.Info[rt].FirstCap); ci < int(pp.Info[rt].LastCap); ci++ {
			c := &pp.Caps[ci]
			cb := collBegin[c.LinkCollection]
			first, last := capBits(c)
			order := childOrder[c.LinkCollection]
			for k := 0; k < directChilds[c.LinkCollection]; k++ {
				if first < collBits[order[k]][c.ReportID][rt].first {
					break
				}
				cb = collEnd[order[k]]
			}
			at := rdSearch(cb, first, rdMain(rt), c.ReportID)
			rdInsertAfter(at, rdNode{firstBit: first, lastBit: last, capsIndex: ci, collIndex: int(c.LinkCollection), main: rdMain(rt), reportID: c.ReportID})
		}
	}

	// Padding: fill bit gaps, pad each report to a byte boundary.
	{
		var lastBitPos [ppReportTypes][256]int
		var lastItem [ppReportTypes][256]*rdNode
		for t := range lastBitPos {
			for r := range lastBitPos[t] {
				lastBitPos[t][r] = -1
			}
		}
		hasReportIDs := false
		var beforeTopEnd *rdNode
		for l := head; l.next != nil; l = l.next {
			if l.main >= rdInput && l.main <= rdFeature && l.firstBit != -1 {
				t, r := int(l.main), l.reportID
				if lastBitPos[t][r]+1 != l.firstBit && lastItem[t][r] != nil && lastItem[t][r].firstBit != l.firstBit {
					at := rdSearch(lastItem[t][r], lastBitPos[t][r], l.main, r)
					rdInsertAfter(at, rdNode{firstBit: lastBitPos[t][r] + 1, lastBit: l.firstBit - 1, padding: true, capsIndex: -1, main: l.main, reportID: r})
				}
				if r != 0 {
					hasReportIDs = true
				}
				lastBitPos[t][r] = l.lastBit
				lastItem[t][r] = l
			}
			if l.next.main == rdCollectionEnd {
				beforeTopEnd = l
			}
		}
		for t := 0; t < ppReportTypes; t++ {
			for r := 0; r < 256; r++ {
				if lastBitPos[t][r] == -1 {
					continue
				}
				pad := 8 - ((lastBitPos[t][r] + 1) % 8)
				if pad < 8 {
					n := rdInsertAfter(lastItem[t][r], rdNode{firstBit: lastBitPos[t][r] + 1, lastBit: lastBitPos[t][r] + pad, padding: true, capsIndex: -1, main: rdMain(t), reportID: uint8(r)})
					if lastItem[t][r] == beforeTopEnd {
						beforeTopEnd = n
					}
					lastItem[t][r] = n
					lastBitPos[t][r] += pad
				}
			}
		}
		for t := 0; t < ppReportTypes; t++ {
			if !hasReportIDs && pp.Info[t].NumberOfCaps > 0 && pp.Info[t].ReportByteLength > 0 && lastBitPos[t][0] != -1 {
				pad := (int(pp.Info[t].ReportByteLength)-1)*8 - (lastBitPos[t][0] + 1)
				if pad > 0 && beforeTopEnd != nil {
					rdInsertAfter(beforeTopEnd, rdNode{firstBit: lastBitPos[t][0] + 1, lastBit: lastBitPos[t][0] + pad, padding: true, capsIndex: -1, main: rdMain(t), reportID: 0})
				}
			}
		}
	}

	// Encode.
	var out []byte
	var lastReportID uint8
	var lastUsagePage uint16
	var lastPhysMin, lastPhysMax int32
	var lastUnitExp, lastUnit uint32
	reportCount := 0
	w := func(item byte, data int64) error { return rdWriteItem(&out, item, data) }
	for n := head; n != nil; n = n.next {
		rt := n.main
		switch {
		case n.main == rdCollection:
			l := &pp.Links[n.collIndex]
			if lastUsagePage != l.LinkUsagePage {
				if err := w(rdItemUsagePage, int64(l.LinkUsagePage)); err != nil {
					return nil, err
				}
				lastUsagePage = l.LinkUsagePage
			}
			if err := w(rdItemUsage, int64(l.LinkUsage)); err != nil {
				return nil, err
			}
			if err := w(rdItemCollection, int64(l.CollectionType)); err != nil {
				return nil, err
			}
		case n.main == rdCollectionEnd:
			if err := w(rdItemEndColl, 0); err != nil {
				return nil, err
			}
		case n.padding:
			bits := n.lastBit - n.firstBit + 1
			if bits%8 == 0 {
				if err := w(rdItemReportSize, 8); err != nil {
					return nil, err
				}
				if err := w(rdItemReportCount, int64(bits/8)); err != nil {
					return nil, err
				}
			} else {
				if err := w(rdItemReportSize, int64(bits)); err != nil {
					return nil, err
				}
				if err := w(rdItemReportCount, 1); err != nil {
					return nil, err
				}
			}
			var mi byte
			switch rt {
			case rdInput:
				mi = rdItemInput
			case rdOutput:
				mi = rdItemOutput
			default:
				mi = rdItemFeature
			}
			if err := w(mi, 0x03); err != nil {
				return nil, err
			}
			reportCount = 0
		default:
			c := &pp.Caps[n.capsIndex]
			if lastReportID != c.ReportID {
				if err := w(rdItemReportID, int64(c.ReportID)); err != nil {
					return nil, err
				}
				lastReportID = c.ReportID
			}
			if c.UsagePage != lastUsagePage {
				if err := w(rdItemUsagePage, int64(c.UsagePage)); err != nil {
					return nil, err
				}
				lastUsagePage = c.UsagePage
			}
			if c.IsButtonCap && c.IsRange {
				reportCount += int(c.DataIndexMax) - int(c.DataIndexMin)
			}
			var err error
			if c.IsRange {
				if err = w(rdItemUsageMin, int64(c.UsageMin)); err == nil {
					err = w(rdItemUsageMax, int64(c.UsageMax))
				}
			} else {
				err = w(rdItemUsage, int64(c.UsageMin))
			}
			if err != nil {
				return nil, err
			}
			if c.IsDesignatorRange {
				if err = w(rdItemDesigMin, int64(c.DesignatorMin)); err == nil {
					err = w(rdItemDesigMax, int64(c.DesignatorMax))
				}
			} else if c.DesignatorMin != 0 {
				err = w(rdItemDesigIndex, int64(c.DesignatorMin))
			}
			if err != nil {
				return nil, err
			}
			if c.IsStringRange {
				if err = w(rdItemStringMin, int64(c.StringMin)); err == nil {
					err = w(rdItemStringMax, int64(c.StringMax))
				}
			} else if c.StringMin != 0 {
				err = w(rdItemString, int64(c.StringMin))
			}
			if err != nil {
				return nil, err
			}

			mainItem := func() error {
				switch rt {
				case rdInput:
					return w(rdItemInput, int64(c.BitField))
				case rdOutput:
					return w(rdItemOutput, int64(c.BitField))
				}
				return w(rdItemFeature, int64(c.BitField))
			}
			nx := n.next
			sameKind := nx != nil && nx.main == rt && !nx.padding && nx.capsIndex >= 0
			if c.IsButtonCap {
				if sameKind && pp.Caps[nx.capsIndex].IsButtonCap && !c.IsRange && !pp.Caps[nx.capsIndex].IsRange &&
					pp.Caps[nx.capsIndex].UsagePage == c.UsagePage && pp.Caps[nx.capsIndex].ReportID == c.ReportID &&
					pp.Caps[nx.capsIndex].BitField == c.BitField {
					if nx.firstBit != n.firstBit {
						reportCount++
					}
					continue
				}
				if c.ButtonLogicalMin == 0 && c.ButtonLogicalMax == 0 {
					if err = w(rdItemLogicalMin, 0); err == nil {
						err = w(rdItemLogicalMax, 1)
					}
				} else {
					if err = w(rdItemLogicalMin, int64(c.ButtonLogicalMin)); err == nil {
						err = w(rdItemLogicalMax, int64(c.ButtonLogicalMax))
					}
				}
				if err != nil {
					return nil, err
				}
				if err = w(rdItemReportSize, int64(c.ReportSize)); err != nil {
					return nil, err
				}
				rc := int(c.ReportCount)
				if !c.IsRange {
					rc += reportCount
				}
				if err = w(rdItemReportCount, int64(rc)); err != nil {
					return nil, err
				}
				if lastPhysMin != 0 {
					lastPhysMin = 0
					_ = w(rdItemPhysicalMin, 0)
				}
				if lastPhysMax != 0 {
					lastPhysMax = 0
					_ = w(rdItemPhysicalMax, 0)
				}
				if lastUnitExp != 0 {
					lastUnitExp = 0
					_ = w(rdItemUnitExp, 0)
				}
				if lastUnit != 0 {
					lastUnit = 0
					_ = w(rdItemUnit, 0)
				}
				if err = mainItem(); err != nil {
					return nil, err
				}
				reportCount = 0
				continue
			}

			// Value cap.
			if c.BitField&0x02 != 0x02 {
				c.ReportCount = c.DataIndexMax - c.DataIndexMin + 1
			}
			if sameKind && !pp.Caps[nx.capsIndex].IsButtonCap && !c.IsRange && !pp.Caps[nx.capsIndex].IsRange {
				o := &pp.Caps[nx.capsIndex]
				if o.UsagePage == c.UsagePage && o.LogicalMin == c.LogicalMin && o.LogicalMax == c.LogicalMax &&
					o.PhysicalMin == c.PhysicalMin && o.PhysicalMax == c.PhysicalMax && o.UnitsExp == c.UnitsExp &&
					o.Units == c.Units && o.ReportSize == c.ReportSize && o.ReportID == c.ReportID &&
					o.BitField == c.BitField && o.ReportCount == 1 && c.ReportCount == 1 {
					reportCount++
					continue
				}
			}
			if err = w(rdItemLogicalMin, int64(c.LogicalMin)); err != nil {
				return nil, err
			}
			if err = w(rdItemLogicalMax, int64(c.LogicalMax)); err != nil {
				return nil, err
			}
			if lastPhysMin != c.PhysicalMin || lastPhysMax != c.PhysicalMax {
				if err = w(rdItemPhysicalMin, int64(c.PhysicalMin)); err != nil {
					return nil, err
				}
				lastPhysMin = c.PhysicalMin
				if err = w(rdItemPhysicalMax, int64(c.PhysicalMax)); err != nil {
					return nil, err
				}
				lastPhysMax = c.PhysicalMax
			}
			if lastUnitExp != c.UnitsExp {
				if err = w(rdItemUnitExp, int64(c.UnitsExp)); err != nil {
					return nil, err
				}
				lastUnitExp = c.UnitsExp
			}
			if lastUnit != c.Units {
				if err = w(rdItemUnit, int64(c.Units)); err != nil {
					return nil, err
				}
				lastUnit = c.Units
			}
			if err = w(rdItemReportSize, int64(c.ReportSize)); err != nil {
				return nil, err
			}
			if err = w(rdItemReportCount, int64(int(c.ReportCount)+reportCount)); err != nil {
				return nil, err
			}
			if err = mainItem(); err != nil {
				return nil, err
			}
			reportCount = 0
		}
	}
	return out, nil
}

// reportIDs returns, per report type, the set of report IDs the blob declares.
func (pp *ppData) reportIDs() [ppReportTypes]map[uint8]bool {
	var out [ppReportTypes]map[uint8]bool
	for rt := 0; rt < ppReportTypes; rt++ {
		out[rt] = map[uint8]bool{}
		for ci := int(pp.Info[rt].FirstCap); ci < int(pp.Info[rt].LastCap) && ci < len(pp.Caps); ci++ {
			out[rt][pp.Caps[ci].ReportID] = true
		}
	}
	return out
}
