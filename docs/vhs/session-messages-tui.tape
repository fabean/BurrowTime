Output assets/burrowtime-session-messages-tui.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 18ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-session-messages-tui/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-session-messages-tui/watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-session-messages-tui && mkdir -p /tmp/burrowtime-vhs-session-messages-tui/burrowtime /tmp/burrowtime-vhs-session-messages-tui/watson"
Enter
Type "from1=$(date +%Y-%m-%dT09:00:00); to1=$(date +%Y-%m-%dT10:30:00)"
Enter
Type "./bin/burrowtime add --from $from1 --to $to1 Demo +DEMO-101 -m 'Investigating import issue' >/dev/null"
Enter
Type "from2=$(date +%Y-%m-%dT10:45:00); to2=$(date +%Y-%m-%dT11:15:00)"
Enter
Type "./bin/burrowtime add --from $from2 --to $to2 Demo +DEMO-101 -m 'Fixed mapping and added regression tests' >/dev/null"
Enter
Type "from3=$(date +%Y-%m-%dT11:30:00); to3=$(date +%Y-%m-%dT12:00:00)"
Enter
Type "./bin/burrowtime add --from $from3 --to $to3 Sample +SAMPLE-2 -m 'Completed QA validation' >/dev/null"
Enter
Ctrl+L
Show

Type "./bin/burrowtime"
Enter
Sleep 3s

Type "l"
Sleep 4s

Type "m"
Sleep 2s
Type "m"
Sleep 3s

Tab
Sleep 4s

Type "m"
Sleep 5s

Type "q"
Sleep 1s
Hide
Type "rm -rf /tmp/burrowtime-vhs-session-messages-tui"
Enter
Sleep 1s
