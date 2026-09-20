package store

import "testing"

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.AddMovie(603, "a")
	s.AddMovie(603, "a")
	s.AddMovie(603, "b")
	s.AddShow(81189, ShowSub{User: "a", Season: 2, Have: 3})
	s.AddShow(81189, ShowSub{User: "a", Season: 2})
	s.AddShow(81189, ShowSub{User: "a", Season: 6, Future: true})
	s.AddShow(81189, ShowSub{User: "a", Season: 7, Future: true})

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Movies()[603]; len(got) != 2 {
		t.Fatalf("movies = %v", got)
	}
	if got := s.Shows()[81189]; len(got) != 2 || got[0].Have != 3 || got[1].Season != 6 {
		t.Fatalf("shows = %+v", got)
	}
	if !s.HasShow(81189, ShowSub{User: "a", Future: true}) || s.HasShow(81189, ShowSub{User: "b", Season: 2}) {
		t.Fatal("HasShow mismatch")
	}

	s.RemoveMovie(603, []string{"a", "b"})
	s.RemoveShow(81189, []ShowSub{{User: "a", Season: 2}})
	if len(s.Movies()) != 0 || len(s.Shows()[81189]) != 1 {
		t.Fatalf("after remove: %v %v", s.Movies(), s.Shows())
	}
}
