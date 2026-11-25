package session

import "testing"

func TestRequestBrowseHasIndependentGeneration(t *testing.T) {
	var requests RequestState
	search := requests.Begin(RequestDiscovery)
	browse := requests.Begin(RequestBrowse)
	if search == 0 || browse <= search {
		t.Fatalf("search/browse generations = %d/%d", search, browse)
	}
	if requests.Active(RequestDiscovery) != search || requests.Active(RequestBrowse) != browse {
		t.Fatalf("active generations = %d/%d", requests.Active(RequestDiscovery), requests.Active(RequestBrowse))
	}
	if requests.Cancel(RequestBrowse) != browse || requests.Active(RequestBrowse) != 0 {
		t.Fatal("browse cancellation did not clear only the browse generation")
	}
	if requests.Active(RequestDiscovery) != search {
		t.Fatal("cancelling browse changed the search generation")
	}
}
