Output assets/burrowtime-session-wizard.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 18ms
Set CursorBlink false
Set Theme { "name": "BurrowTime", "black": "#10251d", "red": "#db705d", "green": "#7ad596", "yellow": "#f0c25d", "blue": "#75a9cf", "magenta": "#be91c6", "cyan": "#75c7b3", "white": "#d9e9db", "brightBlack": "#789587", "brightRed": "#ed8976", "brightGreen": "#96e7ab", "brightYellow": "#f6d078", "brightBlue": "#91bee0", "brightMagenta": "#d1a6d8", "brightCyan": "#94ddcd", "brightWhite": "#fffdf8", "background": "#10251d", "foreground": "#d9e9db", "selectionBackground": "#28523f", "cursorColor": "#f0c25d" }

Hide
Type "export BURROWTIME_DIR=/tmp/burrowtime-vhs-session-wizard/burrowtime"
Enter
Type "export WATSON_DIR=/tmp/burrowtime-vhs-session-wizard/watson"
Enter
Type "export TZ=UTC"
Enter
Type "rm -rf /tmp/burrowtime-vhs-session-wizard && mkdir -p /tmp/burrowtime-vhs-session-wizard/burrowtime /tmp/burrowtime-vhs-session-wizard/watson"
Enter
Type "./bin/burrowtime add --from '2026-10-24 09:00' --to '2026-10-24 09:30' Demo +DEMO-101 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-25 09:00' --to '2026-10-25 09:30' Sample +SAMPLE-2 >/dev/null"
Enter
Type "./bin/burrowtime add --from '2026-10-26 09:00' --to '2026-10-26 09:30' Planning +PLAN-8 >/dev/null"
Enter
Ctrl+L
Show

Type "./bin/burrowtime start -i"
Enter
Sleep 3s
Down
Sleep 1s
Enter
Sleep 2s
Tab
Sleep 1s
Enter
Sleep 2s
Enter
Sleep 3s
Enter
Sleep 3s

Hide
Type "./bin/burrowtime stop >/dev/null"
Enter
Show
Sleep 1s

Type "./bin/burrowtime add -i"
Enter
Sleep 3s
Type "Research"
Sleep 1s
Ctrl+N
Sleep 1s
Type "NEW-TASK-42"
Enter
Sleep 2s
Type "2026-10-27 13:00"
Enter
Sleep 2s
Type "2026-10-27 14:30"
Enter
Sleep 3s
Enter
Sleep 4s

Hide
Type "rm -rf /tmp/burrowtime-vhs-session-wizard"
Enter
Sleep 1s
