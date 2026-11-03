Output assets/burrowtime-aggregate-tui.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 18ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-aggregate-tui/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-aggregate-tui/watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-aggregate-tui && mkdir -p /tmp/burrowtime-vhs-aggregate-tui/burrowtime /tmp/burrowtime-vhs-aggregate-tui/watson"
Enter
Type "./bin/burrowtime add --from '2026-10-26 09:00' --to '2026-10-26 10:30' Demo +DEMO-101 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-26 10:45' --to '2026-10-26 11:30' Demo +DEMO-102 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-26 12:00' --to '2026-10-26 12:30' Sample +SAMPLE-2 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-27 09:00' --to '2026-10-27 10:00' Demo +DEMO-101 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-27 10:15' --to '2026-10-27 10:30' Planning >/dev/null"
Enter
Ctrl+L
Show

Type "./bin/burrowtime"
Enter
Sleep 3s

Down
Down
Enter
Sleep 2s

Right
Right
Right
Sleep 5s

Type "q"
Sleep 1s
Hide
Type "rm -rf /tmp/burrowtime-vhs-aggregate-tui"
Enter
Sleep 1s
