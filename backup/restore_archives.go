package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/omarshahine/trakt-plugin/api"
)

type restoreArchives struct {
	source, current         *comparisonArchive
	sourceHash, currentHash string
	account                 api.RestoreAccount
}

func readRestoreArchives(source, current string) (restoreArchives, error) {
	r := restoreArchives{}
	a, sourceHash, err := readRestoreArchive(source)
	if err != nil {
		return r, fmt.Errorf("source: %w", err)
	}
	b, currentHash, err := readRestoreArchive(current)
	if err != nil {
		return r, fmt.Errorf("current: %w", err)
	}
	aID, err := comparisonAccount(a)
	if err != nil {
		return r, err
	}
	bID, err := comparisonAccount(b)
	if err != nil {
		return r, err
	}
	if _, err := matchComparisonAccount(aID, bID); err != nil {
		return r, err
	}
	if aID.uuid == "" || bID.uuid == "" || aID.uuid != bID.uuid || strings.TrimSpace(bID.slug) == "" {
		return r, fmt.Errorf("restoration requires matching UUIDs in both archives and a current account slug")
	}
	return restoreArchives{source: a, current: b, sourceHash: sourceHash, currentHash: currentHash, account: api.RestoreAccount{Slug: bID.slug, UUID: bID.uuid}}, nil
}

func readRestoreArchive(filename string) (*comparisonArchive, string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open archive")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCompareArchive {
		return nil, "", fmt.Errorf("archive must be a regular file within size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCompareArchive+1))
	if err != nil || len(data) > maxCompareArchive {
		return nil, "", fmt.Errorf("archive read/size failure")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", fmt.Errorf("cannot parse ZIP")
	}
	a, err := readComparisonZIP(z)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return a, hex.EncodeToString(hash[:]), nil
}
