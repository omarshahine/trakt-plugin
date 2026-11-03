package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Restore preflight/readback must not interpret null, missing headers or partial
// pages as complete absence. Walk pages without retaining all raw responses.
func (c *APIClient) walkStrictRecords(path string, requestedLimit, maxRecords int, visit func(json.RawMessage) error) error {
	if requestedLimit < 1 || requestedLimit > 1000 || maxRecords < 1 || visit == nil {
		return fmt.Errorf("invalid bounded record walk")
	}
	pages, limit, total, count := 0, 0, 0, 0
	for page := 1; page <= 100; page++ {
		rows, p, err := c.strictRecordPage(path, page, requestedLimit)
		if err != nil {
			return err
		}
		nums := []int{}
		for _, s := range []string{p.Page, p.PageCount, p.Limit, p.ItemCount} {
			n, e := strconv.Atoi(s)
			if e != nil {
				return fmt.Errorf("missing or invalid record pagination")
			}
			nums = append(nums, n)
		}
		if page == 1 && nums[0] == 1 && nums[1] == 0 && nums[2] >= 1 && nums[2] <= 1000 && nums[3] == 0 && len(rows) == 0 {
			return nil
		}
		if nums[0] != page || nums[1] < 1 || nums[1] > 100 || nums[2] < 1 || nums[2] > 1000 || nums[3] < 0 || nums[3] > maxRecords {
			return fmt.Errorf("record pagination exceeds safe bounds")
		}
		if page == 1 {
			pages, limit, total = nums[1], nums[2], nums[3]
		} else if pages != nums[1] || limit != nums[2] || total != nums[3] {
			return fmt.Errorf("record pagination changed during read")
		}
		wantPages := (total + limit - 1) / limit
		if wantPages == 0 {
			wantPages = 1
		}
		wantRows := limit
		if page == pages {
			wantRows = total - limit*(pages-1)
		}
		if pages != wantPages || len(rows) != wantRows {
			return fmt.Errorf("record page count/size mismatch")
		}
		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
			count++
		}
		if page == pages {
			if count != total {
				return fmt.Errorf("incomplete record scope")
			}
			return nil
		}
	}
	return fmt.Errorf("record page limit exceeded")
}

func (c *APIClient) strictRecordPage(path string, page, limit int) ([]json.RawMessage, Pagination, error) {
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, noCache: true, pagination: PaginationsParams{Page: page, Limit: limit}})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, Pagination{}, fmt.Errorf("record read returned HTTP %d", r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, Pagination{}, fmt.Errorf("record page unreadable or too large")
	}
	var rows []json.RawMessage
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return nil, Pagination{}, fmt.Errorf("record page must be a JSON array, not null")
	}
	return rows, Pagination{Page: r.Header.Get("X-Pagination-Page"), Limit: r.Header.Get("X-Pagination-Limit"), PageCount: r.Header.Get("X-Pagination-Page-Count"), ItemCount: r.Header.Get("X-Pagination-Item-Count")}, nil
}
