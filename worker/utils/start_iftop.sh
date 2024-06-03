# -s 1:         iftop reads bandwidth then exit in 1 second
# -f "port $1": iftop reads bandwidth of port $1 (command line argument)
# -t:           use text interface without ncurses
sudo iftop -t -s 1 -f "port $1" 2>/dev/null | awk '/send and receive/ {print $6}' 