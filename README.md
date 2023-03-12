# Volley

Volley is a simple tool that moves files and/or directories from the source directory to the destination directory.

It is intended to be used with a source directory that is shared over the network to users, and by using Linux
fanotify events it tries to determine when the client has finished writing to the file or directory before moving
it to the destination.