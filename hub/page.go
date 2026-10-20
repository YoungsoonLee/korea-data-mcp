package hub

// PageParams is embedded in tool inputs that return lists, so every list tool
// takes the same page/page_size arguments regardless of the upstream API's
// own names (pageNo, numOfRows, page_count, display, start, ...).
type PageParams struct {
	Page     int `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
	PageSize int `json:"page_size,omitempty" jsonschema:"items per page (default and max depend on the tool)"`
}

// Resolve applies defaults and bounds. It returns an INVALID_ARGUMENT error
// for out-of-range values rather than silently clamping, so the model learns
// the real limits.
func (p PageParams) Resolve(defaultSize, maxSize int) (page, size int, err error) {
	page, size = p.Page, p.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = defaultSize
	}
	if page < 1 {
		return 0, 0, Errorf(CodeInvalidArgument, "page must be >= 1, got %d", page)
	}
	if size < 1 || size > maxSize {
		return 0, 0, Errorf(CodeInvalidArgument, "page_size must be between 1 and %d, got %d", maxSize, size)
	}
	return page, size, nil
}

// PageInfo is the standard pagination block in list outputs.
type PageInfo struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalCount int  `json:"total_count"`
	HasNext    bool `json:"has_next"`
}

func NewPageInfo(page, size, total int) PageInfo {
	return PageInfo{Page: page, PageSize: size, TotalCount: total, HasNext: page*size < total}
}

// List is the standard list output: {"items": [...], "page_info": {...}}.
type List[T any] struct {
	Items    []T      `json:"items"`
	PageInfo PageInfo `json:"page_info"`
}
