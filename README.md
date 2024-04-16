# Quickfile

A simple website that lets a small number of people upload files to your server.
- Anyone can browse and download files
- Uploads only possible through an account
- Accounts are simple private strings (use a guid or something)
- Each account has its own configurable limits
- Limit total size of all files
- Rate limiting
- Per-file expiration (or not, if you want)
- Only writes a single file; no files stored directly on filesystem

## More about

The goal was to write a very basic file share for me and my friends. I thought it'd
be neat to store all the files in a database. I'm aware of the issues involved, but
couldn't resist the idea of only requiring a single data file. No path issues, no 
permission issues, and the database file could technically go anywhere. In fact,
the system only requires four files total: the executable, the config file, the 
index template (separate so you can make up your own page / styling), and the 
single file database. Stick it all in a folder and host it wherever you want.

## Umm

Chances are you probably want to use something else; there are better alternatives.
This was made for fun!
