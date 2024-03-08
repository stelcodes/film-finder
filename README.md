# Film Finder

A web scraping program that finds classic movie screenings and prints them out.

## TODO

### Parallelize all movie fetching

I think the way to do this is in Go is to pass channels into almost every function as a parameter with the input type of the Movie struct. Functions won't return a Movie struct, they will just push any new Movie structs down the channel. And a single WaitGroup can be used to keep track of how many active go processes there are. So that will need to be passed around too. With two new parameters for most functions, should I put both of them into a single Context struct? Is that what the ctx argument does normally in Go? I'll have to read up on that more.
