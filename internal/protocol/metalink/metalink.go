package metalink

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/smartass08/aria2go/internal/hash"
)

const (
	nsV3 = "http://www.metalinker.org/"
	nsV4 = "urn:ietf:params:xml:ns:metalink"
)

const lowestPriority = 999999

type Doc struct {
	Files []File
}

type File struct {
	Name          string
	Size          int64
	SizeKnown     bool
	URLs          []URLEntry
	Hashes        map[hash.Kind][]byte
	Pieces        [][]byte
	PieceHashKind hash.Kind
	PieceLength   int64
	Languages     []string
	OSes          []string
	Version       string
}

type URLEntry struct {
	URL      string
	Type     string
	Priority int
	Location string
}

func Parse(r io.Reader) (*Doc, error) {
	dec := xml.NewDecoder(r)

	tok, err := nextStartElement(dec)
	if err != nil {
		return nil, err
	}
	if tok.Name.Local != "metalink" {
		return nil, fmt.Errorf("metalink: root element is %q, expected <metalink>", tok.Name.Local)
	}

	switch tok.Name.Space {
	case nsV4:
		return parseV4(dec, tok)
	case nsV3:
		return parseV3(dec, tok)
	default:
		return nil, fmt.Errorf("metalink: unsupported namespace %q", tok.Name.Space)
	}
}

func ParseV4(r io.Reader) (*Doc, error) {
	dec := xml.NewDecoder(r)
	tok, err := nextStartElement(dec)
	if err != nil {
		return nil, err
	}
	if tok.Name.Local != "metalink" || tok.Name.Space != nsV4 {
		return nil, fmt.Errorf("metalink: expected v4 namespace, got %q", tok.Name.Space)
	}
	return parseV4(dec, tok)
}

func parseV4(dec *xml.Decoder, root xml.StartElement) (*Doc, error) {
	doc := &Doc{}

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local == "file" && se.Name.Space == nsV4 {
			f, ok := parseFileV4(dec, se)
			if ok {
				doc.Files = append(doc.Files, f)
			}
			continue
		}

		skipElement(dec)
	}

	return doc, nil
}

func parseFileV4(dec *xml.Decoder, se xml.StartElement) (File, bool) {
	f := File{
		Hashes: make(map[hash.Kind][]byte, 2),
	}

	name := attrValue(se, "", "name")
	if name == "" || detectDirTraversal(name) {
		skipElement(dec)
		return f, false
	}
	f.Name = name

	for {
		tok, err := dec.Token()
		if err != nil {
			return f, false
		}

		switch t := tok.(type) {
		case xml.EndElement:
			return f, true

		case xml.StartElement:
			if t.Name.Space != nsV4 {
				skipElement(dec)
				continue
			}

			switch t.Name.Local {
			case "size":
				var size int64
				if err := dec.DecodeElement(&size, &t); err == nil && size >= 0 {
					f.Size = size
					f.SizeKnown = true
				} else {
					return f, false
				}

			case "url":
				u, ok := parseURLV4(dec, t)
				if ok {
					f.URLs = append(f.URLs, u)
				}

			case "hash":
				h := parseHashV4(dec, t)
				if h != nil {
					k, err := hash.Parse(h.Type)
					if err == nil {
						digest, err := hexToBytes(h.Hash)
						if err == nil && len(digest) == k.Size() {
							f.Hashes[k] = digest
						}
					}
				}

			case "pieces":
				pieces := parsePiecesV4(dec, t)
				if pieces.Valid && strongerHashKind(pieces.Kind, f.PieceHashKind) {
					f.PieceLength = pieces.Length
					f.PieceHashKind = pieces.Kind
					f.Pieces = pieces.Hashes
				}

			case "version":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.Version = s
				}

			case "language":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.Languages = append(f.Languages, s)
				}

			case "os":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.OSes = append(f.OSes, s)
				}

			default:
				skipElement(dec)
			}
		}
	}
}

func parseURLV4(dec *xml.Decoder, se xml.StartElement) (URLEntry, bool) {
	u := URLEntry{
		Priority: lowestPriority,
	}

	u.Location = attrValue(se, "", "location")

	if p := attrValue(se, "", "priority"); p != "" {
		prio, err := strconv.Atoi(p)
		if err != nil || prio < 1 || prio > lowestPriority {
			skipElement(dec)
			return u, false
		}
		u.Priority = prio
	}

	if err := dec.DecodeElement(&u.URL, &se); err != nil {
		u.URL = ""
	}

	u.Type = detectScheme(u.URL)

	return u, true
}

type v4Hash struct {
	Type string `xml:"type,attr"`
	Hash string `xml:",chardata"`
}

func parseHashV4(dec *xml.Decoder, se xml.StartElement) *v4Hash {
	var h v4Hash
	if err := dec.DecodeElement(&h, &se); err != nil {
		return nil
	}
	return &h
}

type piecesResult struct {
	Valid  bool
	Kind   hash.Kind
	Length int64
	Hashes [][]byte
}

type v4Pieces struct {
	Type   string   `xml:"type,attr"`
	Length int64    `xml:"length,attr"`
	Hashes []string `xml:"hash"`
}

func parsePiecesV4(dec *xml.Decoder, se xml.StartElement) piecesResult {
	var p v4Pieces
	if err := dec.DecodeElement(&p, &se); err != nil {
		return piecesResult{}
	}
	if p.Length <= 0 {
		return piecesResult{}
	}

	k, err := hash.Parse(p.Type)
	if err != nil {
		return piecesResult{}
	}
	expectedLen := k.Size() * 2

	var result [][]byte
	for _, h := range p.Hashes {
		h = trimSpace(h)
		if len(h) != expectedLen {
			return piecesResult{}
		}
		digest, err := hexToBytes(h)
		if err != nil {
			return piecesResult{}
		}
		result = append(result, digest)
	}

	return piecesResult{
		Valid:  true,
		Kind:   k,
		Length: p.Length,
		Hashes: result,
	}
}

func parseV3(dec *xml.Decoder, root xml.StartElement) (*Doc, error) {
	doc := &Doc{}

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local == "files" && se.Name.Space == nsV3 {
			parseFilesV3(dec, se, doc)
			continue
		}

		skipElement(dec)
	}

	return doc, nil
}

func parseFilesV3(dec *xml.Decoder, se xml.StartElement, doc *Doc) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "files" && t.Name.Space == nsV3 {
				return
			}

		case xml.StartElement:
			if t.Name.Local == "file" && t.Name.Space == nsV3 {
				f, ok := parseFileV3(dec, t)
				if ok {
					doc.Files = append(doc.Files, f)
				}
				continue
			}
			skipElement(dec)
		}
	}
}

func parseFileV3(dec *xml.Decoder, se xml.StartElement) (File, bool) {
	f := File{
		Hashes: make(map[hash.Kind][]byte, 2),
	}

	name := attrValue(se, "", "name")
	if name == "" || detectDirTraversal(name) {
		skipElement(dec)
		return f, false
	}
	f.Name = name

	for {
		tok, err := dec.Token()
		if err != nil {
			return f, false
		}

		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "file" && t.Name.Space == nsV3 {
				return f, true
			}

		case xml.StartElement:
			if t.Name.Space != nsV3 {
				skipElement(dec)
				continue
			}

			switch t.Name.Local {
			case "size":
				var size int64
				if err := dec.DecodeElement(&size, &t); err == nil && size >= 0 {
					f.Size = size
					f.SizeKnown = true
				}

			case "version":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.Version = s
				}

			case "language":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.Languages = append(f.Languages, s)
				}

			case "os":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					f.OSes = append(f.OSes, s)
				}

			case "resources":
				parseResourcesV3(dec, t, &f)

			case "verification":
				parseVerificationV3(dec, t, &f)

			default:
				skipElement(dec)
			}
		}
	}
}

func parseResourcesV3(dec *xml.Decoder, se xml.StartElement, f *File) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "resources" && t.Name.Space == nsV3 {
				return
			}

		case xml.StartElement:
			if t.Name.Local == "url" && t.Name.Space == nsV3 {
				u, ok := parseURLV3(dec, t)
				if ok {
					f.URLs = append(f.URLs, u)
				}
				continue
			}
			skipElement(dec)
		}
	}
}

func parseURLV3(dec *xml.Decoder, se xml.StartElement) (URLEntry, bool) {
	u := URLEntry{
		Priority: lowestPriority,
	}

	typ := attrValue(se, "", "type")
	if typ == "" {
		skipElement(dec)
		return u, false
	}
	u.Type = typ
	u.Location = attrValue(se, "", "location")

	pref := attrValue(se, "", "preference")
	if pref != "" {
		if v, err := strconv.Atoi(pref); err == nil && v >= 0 {
			u.Priority = 101 - v
		}
	}

	if err := dec.DecodeElement(&u.URL, &se); err != nil {
		u.URL = ""
	}

	return u, true
}

func parseVerificationV3(dec *xml.Decoder, se xml.StartElement, f *File) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "verification" && t.Name.Space == nsV3 {
				return
			}

		case xml.StartElement:
			if t.Name.Space != nsV3 {
				skipElement(dec)
				continue
			}

			switch t.Name.Local {
			case "hash":
				h := parseHashV4(dec, t)
				if h != nil {
					k, err := hash.Parse(h.Type)
					if err == nil {
						digest, err := hexToBytes(h.Hash)
						if err == nil && len(digest) == k.Size() {
							f.Hashes[k] = digest
						}
					}
				}

			case "pieces":
				parsePiecesV3(dec, t, f)

			default:
				skipElement(dec)
			}
		}
	}
}

type v3PieceHash struct {
	Hash string `xml:",chardata"`
}

func parsePiecesV3(dec *xml.Decoder, se xml.StartElement, f *File) {
	length := attrValue(se, "", "length")
	hashType := attrValue(se, "", "type")

	if length == "" {
		skipElement(dec)
		return
	}
	plen, err := strconv.ParseInt(length, 10, 64)
	if err != nil || plen <= 0 {
		skipElement(dec)
		return
	}

	k, err := hash.Parse(hashType)
	if err != nil {
		skipElement(dec)
		return
	}
	if !strongerHashKind(k, f.PieceHashKind) {
		skipElement(dec)
		return
	}
	expectedLen := k.Size() * 2

	type pieceEntry struct {
		order int
		data  string
	}
	var entries []pieceEntry

	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "pieces" && t.Name.Space == nsV3 {
				sort.Slice(entries, func(i, j int) bool {
					return entries[i].order < entries[j].order
				})
				var result [][]byte
				for _, e := range entries {
					digest, err := hexToBytes(e.data)
					if err != nil {
						return
					}
					result = append(result, digest)
				}
				f.PieceLength = plen
				f.PieceHashKind = k
				f.Pieces = result
				return
			}

		case xml.StartElement:
			if t.Name.Local == "hash" && t.Name.Space == nsV3 {
				pieceAttr := attrValue(t, "", "piece")
				if pieceAttr == "" {
					skipElement(dec)
					return
				}
				order, err := strconv.Atoi(pieceAttr)
				if err != nil || order < 0 {
					skipElement(dec)
					return
				}
				var ph v3PieceHash
				if err := dec.DecodeElement(&ph, &t); err == nil {
					ph.Hash = trimSpace(ph.Hash)
					if len(ph.Hash) != expectedLen {
						return
					}
					entries = append(entries, pieceEntry{order: order, data: ph.Hash})
				}
				continue
			}
			skipElement(dec)
		}
	}
}

func detectScheme(rawURL string) string {
	for i := 0; i < len(rawURL); i++ {
		if rawURL[i] == ':' && i+3 <= len(rawURL) && rawURL[i+1] == '/' && rawURL[i+2] == '/' {
			return rawURL[:i]
		}
	}
	return ""
}

func detectDirTraversal(name string) bool {
	for i := 0; i < len(name)-1; i++ {
		if name[i] == '.' && name[i+1] == '.' {
			return true
		}
	}
	return false
}

func strongerHashKind(candidate, current hash.Kind) bool {
	if current == "" {
		return candidate != ""
	}
	return hashStrength(candidate) > hashStrength(current)
}

func hashStrength(k hash.Kind) int {
	switch k {
	case hash.MD5:
		return 1
	case hash.SHA1:
		return 2
	case hash.SHA224:
		return 3
	case hash.SHA256:
		return 4
	case hash.SHA384:
		return 5
	case hash.SHA512:
		return 6
	default:
		return 0
	}
}

func skipElement(dec *xml.Decoder) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		if _, ok := tok.(xml.StartElement); ok {
			depth++
		}
		if _, ok := tok.(xml.EndElement); ok {
			depth--
		}
	}
}

func nextStartElement(dec *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se, nil
		}
	}
}

func attrValue(se xml.StartElement, space, local string) string {
	for _, a := range se.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func hexToBytes(s string) ([]byte, error) {
	s = trimSpace(s)
	return hex.DecodeString(s)
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
