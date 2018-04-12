package gbb

type paginator struct {
	total    int64
	pageSize int
	page     int
}

func (p *paginator) CurrentPage() int {
	return p.page
}

func (p *paginator) PageCount() int {
	return int(p.total)/p.pageSize + 1
}

func (p *paginator) NextPage() int {
	return p.page + 1
}

func (p *paginator) HasNextPage() bool {
	return int64((p.page)*p.pageSize) < p.total
}

func (p *paginator) PrevPage() int {
	if p.page < 1 {
		return 1
	}
	prev := p.page - 1
	if int64(prev*p.pageSize) > p.total {
		return int(p.total) / p.pageSize
	}
	return prev
}

func (p *paginator) HasPrevPage() bool {
	return p.page > 1
}

func (p *paginator) Pages() []PaginatorPage {
	pages := make([]PaginatorPage, p.PageCount())
	for i := range pages {
		pages[i] = PaginatorPage{
			Number:  i + 1,
			Active:  p.page == i+1,
			IsFirst: i == 0,
			IsLast:  i == len(pages)-1,
		}
	}
	return pages
}

type PaginatorPage struct {
	Number  int
	Active  bool
	IsFirst bool
	IsLast  bool
}
