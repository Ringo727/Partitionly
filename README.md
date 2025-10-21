> # Partitionly

***A fun music partitioning, collaborating, and distribution tool for producers***

## Description
This is a tiny web app for “beat battles” and remix exchanges where one person hosts a private lobby (join by short code) and friends hop in, upload one audio file, and at the end the host grabs everything as a ZIP. The core “Sample” mode lets the host share a sample everyone can download, make a remix, then re-upload. We’ll also support collaboration modes: “Collab” (everyone uploads a track that gets randomly swapped), “Cyclic” (swap in a circle so odd group sizes work), “Pair” (random pairs remix each other), and “Telephone” (people take turns iterating on a track in order). The host can force or wait for all swaps to finish, and can choose whether only the host or everyone can download the final ZIP. It’s deliberately simple—one small site with basic pages to join, upload, see the queue, and export—so it’s easy to demo on stream or add nice-to-have features later (like a basic in-browser playlist).

### Modes
**Sample Mode:**
<br>
This mode has everyone in a lobby to download and remix a sample that the host uploads.

**Collab Mode:**
<br>
This mode has everyone to pair up with someone else (or cyclically for odd groups) to remix each other's music.

**Telephone Mode:**
<br>
This mode has everyone become ordered followed by the first person uploading a song, and then having the next person remix
the previous person's song similar to the "telephone" game.