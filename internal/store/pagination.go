package store

// normalizePagination applies the common list defaults and calculates a safe
// offset. An absurdly large page should produce an empty page, not wrap the
// integer multiplication and accidentally return the first page.
func normalizePagination(page, size int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	maxInt := int(^uint(0) >> 1)
	offset := maxInt
	if page-1 <= maxInt/size {
		offset = (page - 1) * size
	}
	return page, size, offset
}
