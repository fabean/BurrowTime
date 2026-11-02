Output assets/burrowtime-session-messages.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 22
Set TypingSpeed 28ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-session-messages/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-session-messages/watson"
Enter
Type "rm -rf /tmp/burrowtime-vhs-session-messages && mkdir -p /tmp/burrowtime-vhs-session-messages/burrowtime /tmp/burrowtime-vhs-session-messages/watson"
Enter
Ctrl+L
Show

Type "./bin/burrowtime start Demo +DEMO-101 -m 'Investigating import issue'"
Enter
Sleep 2s

Type "./bin/burrowtime stop -m 'Fixed mapping and added regression tests'"
Enter
Sleep 2s

Type "./bin/burrowtime add --from '2026-11-02 09:00' --to '2026-11-02 10:30' Demo +DEMO-101 -m 'Completed QA validation'"
Enter
Sleep 2s

Type "./bin/burrowtime aggregate --from 2026-11-02 --to 2026-11-02 --show-messages --no-pager"
Enter
Sleep 5s

Hide
Type "rm -rf /tmp/burrowtime-vhs-session-messages"
Enter
Sleep 1s
