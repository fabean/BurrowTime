Output assets/burrowtime-session-edit-tui.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 18ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-session-edit/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-session-edit/watson"
Enter
Type "export TZ=UTC"
Enter
Type "rm -rf /tmp/burrowtime-vhs-session-edit && mkdir -p /tmp/burrowtime-vhs-session-edit/burrowtime /tmp/burrowtime-vhs-session-edit/watson"
Enter
Type "./bin/burrowtime add --from '2026-10-26 15:00' --to '2026-10-26 15:30' Sample +SAMPLE-2 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-27 07:15:30' --to '2026-10-27 07:30:02' Demo +DEMO-101 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-27 09:00' --to '2026-10-27 10:00' Planning +PLAN-8 >/dev/null"
Enter
Ctrl+L
Show

Type "./bin/burrowtime"
Enter
Sleep 3s
Type "l"
Left
Left
Sleep 3s

Type "e"
Sleep 3s
Down
Sleep 1s
Up
Enter
Sleep 3s

Down
Down
Enter
Ctrl+U
Type "2026-10-27 07:15:00"
Enter
Sleep 2s
Down
Enter
Ctrl+U
Type "2026-10-27 07:30:00"
Enter
Sleep 2s
Ctrl+S
Sleep 3s
Enter
Sleep 4s

Escape
Type "q"
Sleep 1s
Hide
Type "rm -rf /tmp/burrowtime-vhs-session-edit"
Enter
Sleep 1s
