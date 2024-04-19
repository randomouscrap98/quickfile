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

## Building / Running
```
# Have go installed, then
git clone https://github.com/randomouscrap98/quickfile.git
cd quickfile/cmd
go build -o quickfile
./quickfile
```
When you run it, it will automatically create a default `config.toml` which you can modify. The program will not detect changes in the config at runtime, you will need to restart it for changes to take effect.

Once the database is created, you can move it wherever you want, so long as you change the location in the `config.toml`. Databases store all data for the system, including files. Yes this is stupid, I just wanted to lol. Databases are "versioned", so if the format changes as the program updates, older databases won't work. This probably won't be a problem at all though.

Accounts are currently stored inside the `config.toml`. This means adding new users requires reloading the program. This also may change in the future.


## Umm

Chances are you probably want to use something else; there are better alternatives.
This was made for fun!
