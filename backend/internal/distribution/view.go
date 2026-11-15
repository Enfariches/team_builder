package distribution

type UserBrief struct {
	ID        int64
	FirstName string
	LastName  string
	Link      *string
}

type MemberView struct {
	MemberID int64
	User     UserBrief
	Role     string
}

// TeamView: ID пуст у непубликованного прогона — команд в БД ещё нет.
type TeamView struct {
	ID      *int64
	Name    string
	Members []MemberView
}

// RunView — прогон с расшифрованными участниками для ответа API.
type RunView struct {
	Run      Run
	Teams    []TeamView
	Waitlist []UserBrief
}
