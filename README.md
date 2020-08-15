# mwsh

Traverse the bookmarks exported from your browser to see which ones are still alive.

Supported browsers:
1. Chrome
2. Firefox
3. Question: how to design the tool so that it can cover other browsers with as least changes as possible?
    Maybe a simple `Walker` interface can suffice?
    ```
    type Walker interface {
        Walk(root html.Tree) <-chan *url.URL
    }
    ```

marwash aims at providing a better experience for inspecting the existing bookmarked URLs than manual checking, where "better" means less time-consuming.

Because it doesn't aim at providing super-precise checking on URL liveness, marwash must provide opportunity for its user to inspect its checking result. To do so, marwash provides output for retained urls, urls it thinks unreachable, and those whose liveness it is uncertain about(which is common due to authN/Z on server serving the URL).

If the user totally trusts marwash :D ...
```
mwsh original-bookmarks.html -o alive.html
```
where alive.html holds URLs which marwash thinks alive, in the same structure as those in original-bookmarks.html.

For the more skeptical:
```
mwsh original-bookmarks.html -o alive.html -d dead.txt -u uncertain.txt -a alive.txt
```
where dead.txt, uncertain.txt and alive.txt holds URLs marwash thinks dead, uncertain, alive respectively. Note these output are all in form of simple text stream, and the destination is not limited to file, it can be any other sink as well. 

That being said, marwash lets user to merge URLs she thinks alive after examining its output, and produces a "filtered" version of bookmarked URLs whose structure aligned with that of the original input. Now the user only need marwash twice in order to get a cleaned version of bookmarked URLs which is ready to be imported to her browser.

We treat the the collection of URLs which user thinks alive as a whitelist, which is also in form of simple text stream, e.g., a text file each line of which is a URL.

```
mwsh original-bookmarks.html -w whitelist.txt -o cleaned.html
```

For speed, we want as less network IO as possible. Given the first round of marwash run and user inspection can determine the URLs which user needs to preserve, we can simply dump them into a whitelist, feed it to marwash and have it retain the whitelisted links only, along with its position in the original bookmark file:

```
# -s tells marwash not to issue any network request 
mwsh original-bookmarks.html -s -w whitelist.txt -o cleaned.html
``` 

A better idea for default output behavior is that marwash outputs urls it thinks alive to stdout as text stream(one URL per line), and unknown/dead url to stderr, where each line in format of `[status]\t[url]`, and make producing a cleaned bookmark file (which aligned to the structure of input file) optional. I think this is way more unix-ish as it employs simple and clean text stream output, which means other existing programs can work with the output easily.

So what if user would like to know why a specific URL is classified as dead / unknown status? As we have used stdout and stderr to output alive and dead/unknown urls respectively, in order to not mix error details into these output stream we can consider adding an option to output errors/status code to a given file descriptor. A better idea IMO, is to output error/status code along with url status, like following:
```
<status> <url> [respCode] [error] where respCode and error presents if available
example:
Unknown https://mybank.com/login    401
Dead    http://non.existent.domain  Unknown Host
```

A even better idea is to output all the bookmarked URL statuses to stdout, and leave stderr for any errors we encounter and debugging logs. The reasoning being that with existing unix filters like `grep`, `sort` and `awk` a user can get whatever results she want without any hassle, as long as `mwsh` provides output in a way that is easy for these filters to process.
