package arcade

import (
	"fmt"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

type LobbyView struct {
	View
	selectedRow int
}

// const stickmen = []string{
// 	o   \ o /  _ o         __|    \ /     |__        o _  \ o /   o
// 	/|\    |     /\   ___\o   \o    |    o/    o/__   /\     |    /|\
// 	/ \   / \   | \  /)  |    ( \  /o\  / )    |  (\  / |   / \   / \
// }

// const stickmen_list = [][]string{{" o ","/|\\","/ \\"}, {"\\ o /","  |  "," / \\ "}, }

// var simple_man = []string {" o ","/|\\","/ \\"};

const (
	lv_TableX1 = 20
	lv_TableY1 = 4
	lv_TableX2 = 59
	lv_TableY2 = 12
)

var lobby_footer = []string{
	"[S]tart game       [C]ancel",
}

func NewLobbyView() *LobbyView {
	return &LobbyView{}
}

func (v *LobbyView) Init() {
}

func (v *LobbyView) ProcessEvent(evt interface{}) {
	arcade.lobbyMux.RLock()
	defer arcade.lobbyMux.RUnlock()

	switch evt := evt.(type) {
	case *ClientDisconnectEvent:
		arcade.Lobby.RemovePlayer(evt.ClientID)
	case *tcell.EventKey:
		switch evt.Key() {
		case tcell.KeyRune:
			switch evt.Rune() {
			case 'c':
				if arcade.Lobby.HostID != arcade.Server.ID {
					// not the host, just leave the game
					host, _ := arcade.Server.Network.GetClient(arcade.Lobby.HostID)
					arcade.Server.Network.Send(host, NewLeaveMessage(arcade.Server.ID))
				}
				arcade.ViewManager.SetView(NewGamesListView())
				// delete game?
			case 's':
				//start game
				NewGame(arcade.Lobby)
			}
		}
	}
}

func (v *LobbyView) ProcessMessage(from *Client, p interface{}) interface{} {
	arcade.lobbyMux.RLock()
	defer arcade.lobbyMux.RUnlock()

	switch p := p.(type) {
	case HelloMessage:
		return NewLobbyInfoMessage(arcade.Lobby)
	case JoinMessage:
		arcade.Lobby.mu.RLock()
		defer arcade.Lobby.mu.RUnlock()

		if len(arcade.Lobby.PlayerIDs) == arcade.Lobby.Capacity {
			return NewJoinReplyMessage(&Lobby{}, ErrCapacity)
		} else if arcade.Lobby.code != p.Code {
			return NewJoinReplyMessage(&Lobby{}, ErrWrongCode)
		} else {
			arcade.Lobby.mu.RUnlock()
			arcade.Lobby.AddPlayer(p.PlayerID)
			arcade.Lobby.mu.RLock()
			return NewJoinReplyMessage(arcade.Lobby, OK)
		}
	case LeaveMessage:
		arcade.Lobby.RemovePlayer(p.PlayerID)
	}
	return nil
}

func (v *LobbyView) Render(s *Screen) {
	arcade.lobbyMux.RLock()
	defer arcade.lobbyMux.RUnlock()

	width, height := s.displaySize()

	// Green text on default background
	sty := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorGreen)
	sty_bold := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorDarkGreen)

	// Draw GAME header

	game_header := pong_header
	if arcade.Lobby.GameType == Tron {
		game_header = tron_header
	}
	headerX := (width - utf8.RuneCountInString(game_header[0])) / 2
	s.DrawText(headerX, 1, sty, game_header[0])
	s.DrawText(headerX, 2, sty, game_header[1])

	// Draw box surrounding games list
	s.DrawBox(lv_TableX1, lv_TableY1, lv_TableX2, lv_TableY2, sty, true)

	// Draw game info

	// name
	nameHeader := "Name: "
	nameString := arcade.Lobby.Name
	s.DrawText((width-len(nameHeader+nameString))/2, lv_TableY1+1, sty, nameHeader)
	s.DrawText((width-len(nameHeader+nameString))/2+utf8.RuneCountInString(nameHeader), lv_TableY1+1, sty_bold, nameString)

	// private
	privateHeader := "Visibility: "
	privateString := "public"
	if arcade.Lobby.Private {
		privateString = "private, Join Code: " + arcade.Lobby.code
	}
	s.DrawText((width-len(privateHeader+privateString))/2, lv_TableY1+2, sty, privateHeader)
	s.DrawText((width-len(privateHeader+privateString))/2+utf8.RuneCountInString(privateHeader), lv_TableY1+2, sty_bold, privateString)

	// capacity
	capacityHeader := "Game capacity: "
	capacityString := fmt.Sprintf("(%v/%v)", len(arcade.Lobby.PlayerIDs), arcade.Lobby.Capacity)
	s.DrawText((width-len(capacityHeader+capacityString))/2, lv_TableY1+3, sty, capacityHeader)
	s.DrawText((width-len(capacityHeader+capacityString))/2+utf8.RuneCountInString(capacityHeader), lv_TableY1+3, sty_bold, capacityString)

	// Draw people
	s.DrawText((width-len(capacityHeader+capacityString))/2+utf8.RuneCountInString(capacityHeader), lv_TableY1+3, sty_bold, capacityString)

	// Draw footer with navigation keystrokes
	s.DrawText((width-len(lobby_footer[0]))/2, height-2, sty, lobby_footer[0])

}

func (v *LobbyView) Unload() {
}
