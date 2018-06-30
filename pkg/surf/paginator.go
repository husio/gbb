package surf

type Paginator struct {
	Total    int64
	PageSize int
	Page     int
}

func (p *Paginator) CurrentPage() int {
	return p.Page
}

func (p *Paginator) PageCount() int {
	return int(p.Total)/p.PageSize + 1
}

func (p *Paginator) NextPage() int {
	return p.Page + 1
}

func (p *Paginator) HasNextPage() bool {
	return int64((p.Page)*p.PageSize) < p.Total
}

func (p *Paginator) PrevPage() int {
	if p.Page < 1 {
		return 1
	}
	prev := p.Page - 1
	if int64(prev*p.PageSize) > p.Total {
		return int(p.Total) / p.PageSize
	}
	return prev
}

func (p *Paginator) HasPrevPage() bool {
	return p.Page > 1
}

func (p *Paginator) Pages() []PaginatorPage {
	pages := make([]PaginatorPage, p.PageCount())
	for i := range pages {
		pages[i] = PaginatorPage{
			Number:  i + 1,
			Active:  p.Page == i+1,
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
