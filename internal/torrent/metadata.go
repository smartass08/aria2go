package torrent

import (
	"fmt"

	"github.com/smartass08/aria2go/internal/bencode"
)

func FromMetadata(infoRaw []byte, announceList [][]string) ([]byte, error) {
	if len(infoRaw) == 0 {
		return nil, fmt.Errorf("torrent: empty metadata")
	}
	if end, err := scanBencodeValue(infoRaw, 0); err != nil {
		return nil, fmt.Errorf("torrent: invalid metadata: %w", err)
	} else if end != len(infoRaw) {
		return nil, fmt.Errorf("torrent: invalid metadata: trailing data")
	}

	torrent := make([]byte, 0, len(infoRaw)+128)
	torrent = append(torrent, 'd')
	if len(announceList) > 0 {
		torrent = append(torrent, []byte("13:announce-list")...)
		al := bencode.ListVal{}
		for _, tier := range announceList {
			if len(tier) == 0 {
				continue
			}
			values := make([]bencode.Value, 0, len(tier))
			for _, uri := range tier {
				values = append(values, bencode.NewString(uri))
			}
			al.L = append(al.L, bencode.ListVal{L: values})
		}
		if len(al.L) > 0 {
			encoded, err := bencode.Marshal(al)
			if err != nil {
				return nil, err
			}
			torrent = append(torrent, encoded...)
		} else {
			torrent = torrent[:1]
		}
	}
	torrent = append(torrent, []byte("4:info")...)
	torrent = append(torrent, infoRaw...)
	torrent = append(torrent, 'e')
	return torrent, nil
}
