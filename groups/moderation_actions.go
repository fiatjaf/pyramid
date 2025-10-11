package groups

import (
	"fmt"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

var PTagNotValidPublicKey = fmt.Errorf("'p' tag value is not a valid public key")

type Action interface {
	Apply(group *nip29.Group)
	Name() string
}

var (
	_ Action = PutUser{}
	_ Action = RemoveUser{}
	_ Action = CreateGroup{}
	_ Action = DeleteEvent{}
	_ Action = EditMetadata{}
)

func PrepareModerationAction(evt nostr.Event) (Action, error) {
	factory, ok := moderationActionFactories[evt.Kind]
	if !ok {
		return nil, fmt.Errorf("event kind %d is not a supported moderation action", evt.Kind)
	}
	return factory(evt)
}

var moderationActionFactories = map[nostr.Kind]func(nostr.Event) (Action, error){
	nostr.KindSimpleGroupPutUser: func(evt nostr.Event) (Action, error) {
		targets := make([]PubKeyRoles, 0, len(evt.Tags))
		for tag := range evt.Tags.FindAll("p") {
			target, err := nostr.PubKeyFromHex(tag[1])
			if err != nil {
				return nil, PTagNotValidPublicKey
			}

			targets = append(targets, PubKeyRoles{
				PubKey:    target,
				RoleNames: tag[2:],
			})
		}
		if len(targets) > 0 {
			return PutUser{Targets: targets, When: evt.CreatedAt}, nil
		}
		return nil, fmt.Errorf("missing 'p' tags")
	},
	nostr.KindSimpleGroupRemoveUser: func(evt nostr.Event) (Action, error) {
		targets := make([]nostr.PubKey, 0, len(evt.Tags))
		for tag := range evt.Tags.FindAll("p") {
			target, err := nostr.PubKeyFromHex(tag[1])
			if err != nil {
				return nil, PTagNotValidPublicKey
			}

			targets = append(targets, target)
		}
		if len(targets) > 0 {
			return RemoveUser{Targets: targets, When: evt.CreatedAt}, nil
		}
		return nil, fmt.Errorf("missing 'p' tags")
	},
	nostr.KindSimpleGroupEditMetadata: func(evt nostr.Event) (Action, error) {
		ok := false
		edit := EditMetadata{When: evt.CreatedAt}
		if t := evt.Tags.Find("name"); t != nil {
			edit.NameValue = &t[1]
			ok = true
		}
		if t := evt.Tags.Find("picture"); t != nil {
			edit.PictureValue = &t[1]
			ok = true
		}
		if t := evt.Tags.Find("about"); t != nil {
			edit.AboutValue = &t[1]
			ok = true
		}

		y := true
		n := false

		if evt.Tags.Has("public") {
			edit.PrivateValue = &n
			ok = true
		} else if evt.Tags.Has("private") {
			edit.PrivateValue = &y
			ok = true
		}

		if evt.Tags.Has("open") {
			edit.ClosedValue = &n
			ok = true
		} else if evt.Tags.Has("closed") {
			edit.ClosedValue = &y
			ok = true
		}

		if ok {
			return edit, nil
		}
		return nil, fmt.Errorf("missing metadata tags")
	},
	nostr.KindSimpleGroupDeleteEvent: func(evt nostr.Event) (Action, error) {
		missing := true
		targets := make([]nostr.ID, 0, 2)
		for tag := range evt.Tags.FindAll("e") {
			id, err := nostr.IDFromHex(tag[1])
			if err != nil {
				return nil, fmt.Errorf("invalid event id hex")
			}

			targets = append(targets, id)
		}

		if missing {
			return nil, fmt.Errorf("missing 'e' tag")
		}

		return DeleteEvent{Targets: targets}, nil
	},
	nostr.KindSimpleGroupCreateGroup: func(evt nostr.Event) (Action, error) {
		return CreateGroup{Creator: evt.PubKey, When: evt.CreatedAt}, nil
	},
	nostr.KindSimpleGroupDeleteGroup: func(evt nostr.Event) (Action, error) {
		return DeleteGroup{When: evt.CreatedAt}, nil
	},
}

type DeleteEvent struct {
	Targets []nostr.ID
}

func (_ DeleteEvent) Name() string             { return "delete-event" }
func (a DeleteEvent) Apply(group *nip29.Group) {}

type PubKeyRoles struct {
	PubKey    nostr.PubKey
	RoleNames []string
}

type PutUser struct {
	Targets []PubKeyRoles
	When    nostr.Timestamp
}

func (_ PutUser) Name() string { return "put-user" }
func (a PutUser) Apply(group *nip29.Group) {
	for _, target := range a.Targets {
		roles := make([]*nip29.Role, 0, len(target.RoleNames))
		for _, roleName := range target.RoleNames {
			if slices.IndexFunc(roles, func(r *nip29.Role) bool { return r.Name == roleName }) != -1 {
				continue
			}
			roles = append(roles, group.GetRoleByName(roleName))
		}
		group.Members[target.PubKey] = roles
	}
}

type RemoveUser struct {
	Targets []nostr.PubKey
	When    nostr.Timestamp
}

func (_ RemoveUser) Name() string { return "remove-user" }
func (a RemoveUser) Apply(group *nip29.Group) {
	for _, tpk := range a.Targets {
		delete(group.Members, tpk)
	}
}

type EditMetadata struct {
	NameValue    *string
	PictureValue *string
	AboutValue   *string
	PrivateValue *bool
	ClosedValue  *bool
	When         nostr.Timestamp
}

func (_ EditMetadata) Name() string { return "edit-metadata" }
func (a EditMetadata) Apply(group *nip29.Group) {
	group.LastMetadataUpdate = a.When
	if a.NameValue != nil {
		group.Name = *a.NameValue
	}
	if a.PictureValue != nil {
		group.Picture = *a.PictureValue
	}
	if a.AboutValue != nil {
		group.About = *a.AboutValue
	}
	if a.PrivateValue != nil {
		group.Private = *a.PrivateValue
	}
	if a.ClosedValue != nil {
		group.Closed = *a.ClosedValue
	}
}

type CreateGroup struct {
	Creator nostr.PubKey
	When    nostr.Timestamp
}

func (_ CreateGroup) Name() string { return "create-group" }
func (a CreateGroup) Apply(group *nip29.Group) {
	group.LastMetadataUpdate = a.When
	group.LastAdminsUpdate = a.When
	group.LastMembersUpdate = a.When
}

type DeleteGroup struct {
	When nostr.Timestamp
}

func (_ DeleteGroup) Name() string { return "delete-group" }
func (a DeleteGroup) Apply(group *nip29.Group) {
	group.Members = make(map[nostr.PubKey][]*nip29.Role)
	group.Closed = true
	group.Private = true
	group.Name = "[deleted]"
	group.About = ""
	group.Picture = ""
	group.LastMetadataUpdate = a.When
	group.LastAdminsUpdate = a.When
	group.LastMembersUpdate = a.When
}
