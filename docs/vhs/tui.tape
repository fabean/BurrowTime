Output assets/burrowtime-tui.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 18ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-tui"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-tui /tmp/burrowtime-vhs-watson && mkdir -p /tmp/burrowtime-vhs-tui /tmp/burrowtime-vhs-watson"
Enter
Type "from1=$(date +%Y-%m-%dT09:00:00); to1=$(date +%Y-%m-%dT10:35:00)"
Enter
Type "./bin/burrowtime add --from $from1 --to $to1 'client portal' +review >/dev/null"
Enter
Type "from2=$(date +%Y-%m-%dT11:00:00); to2=$(date +%Y-%m-%dT12:12:00)"
Enter
Type "./bin/burrowtime add --from $from2 --to $to2 'agent research' +codex >/dev/null"
Enter
Type "from3=$(date +%Y-%m-%dT13:15:00); to3=$(date +%Y-%m-%dT14:03:00)"
Enter
Type "./bin/burrowtime add --from $from3 --to $to3 documentation +writing >/dev/null"
Enter
Ctrl+L
Show

Type "./bin/burrowtime"
Enter
Sleep 4s

Type "r"
Sleep 4s

Tab
Sleep 3s

Type "/"
Type "client portal"
Enter
Sleep 4s

Type "q"
Sleep 1s
