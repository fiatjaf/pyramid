# pyramid

**pyramid** serves as a wondrous furnace of communityzenship for your Nostr experience, enabling users to build and nurture vibrant communities through a hierarchical relay system. With powerful subrelay features, extensive optional configurations, and easy theming options, **pyramid** makes it effortless to create and manage personalized Nostr environments tailored to your personal or community's needs.

## easy install

type this in a blank server you just rented:

```
curl -s https://raw.githubusercontent.com/fiatjaf/pyramid/refs/heads/master/easy.sh | bash
```

or watch [this](https://fevela.me/nevent1qvzqqqqqqypzqwlsccluhy6xxsr6l9a9uhhxf75g85g8a709tprjcn4e42h053vaqyd8wumn8ghj7urewfsk66ty9enxjct5dfskvtnrdakj7qgmwaehxw309aex2mrp0yh8wetnw3jhymnzw33jucm0d5hsqgzz6ft7cfafp3dw29cyewc4cqhv59cxn392vesfexg0szv73gl06czvu37k) instructive video:



https://github.com/user-attachments/assets/3eafa97c-a7a9-4fdc-b1ea-f466dae47634


## features

- **easy install**
  - a single-line install setup and no need to fiddle with configuration files
  - if you can buy VPS access you can setup one of these
  - lean resource usage, because this is not javascript, cheap servers will go very far

<img width="600" align="left" src="https://github.com/user-attachments/assets/9162cd0f-f442-45f6-a505-f1771e6b5ab4" />

- **hierarchical membership system**
  - members can invite other members, up to a configurable number of invites
  - every member is responsible by all its children and descendants, and can decide to kick them out anytime
  - a log of invites and drops is kept for rebuilding state and clarifying confusions
  - a member can be invited by more than one parent at the same time, safeguarding them from despotic future drops
  - a self-organizing system that can scale relay membership to thousands
  - anyone can leave anytime, breaking their links in the ladder
  - adding and dropping can be done through the web UI or using standard relay management tools
  - two-step standardized invite codes interface combined with event-based join requests also works

<br clear="all">

<img align="right" width="400" src="https://github.com/user-attachments/assets/f53b7e34-6be1-45be-802a-fa17df3a4b7f" />
<img align="right" width="400" src="https://github.com/user-attachments/assets/9b3979a3-4ab5-4723-8df2-696c74fd83c3" />
<img align="right" width="400" src="https://github.com/user-attachments/assets/7f2d9bb0-505b-475c-b75e-0bca843f9831" />

- **custom-featured multi-relays**
  - each relay listens in its own HTTP path and can be treated as completely independent
    - some are useful for members, others are useful for externals, others are like services an inner group of a community can provide to its external members
    - storage is shared in a single memory-mapped file for very fast access and automatic disk-saving deduplication, but indexes are independent so there is no risk of mixing events
  - _main_: the basic pyramid relay functionality
    - listens at the top-level path
    - only members can publish
    - also accepts zaps issued by relay members even though these are signed by zapper services
  - _internal_: a relay private to members of the hierarchy, both for reading and for writing
  - _favorites_: notes from external users manually curated by relay members through republishing chosen events
  - _inbox_: a safe inbox with protection against hellthreads and spam, with
    - filtering out anyone outside the extended (2-level) social graph of relay members
    - custom bans invalidate specific users and their social graph
    - optional proof-of-work requirements
  - _popular_: notes from external users automatically curated by relay members based on reactions and interactions
  - _uppermost_: only the notes most loved by a higher percentage of relay members
  - _moderated_: a multi-use relay open to the public, but for which pyramid members have to approve each post manually
  - _groups_: a relay that also listens at the top-level path, but provides moderated group functionality
    - members can create groups and they become admins of such groups
    - non-pyramid members can join these groups, provided that their admins allow
    - groups can be private, in which case messages will only be shown to members of each group
    - invite code functionality also supported
    - pyramid root admin can see all the groups and moderate them

<br clear="all">

<img align="left" width="400" src="https://github.com/user-attachments/assets/9bb08e0d-29b7-48dc-817a-c5a06c2418bb" />

- **extensive optional configurations**
  - almost everything is configurable from the UI
  - from relay metadata to numeric settings, for both the main relay and for all sub-relays
  - event the path under which each sub-relay listens can be (dangerously) changed
  - smart defaults allow you to get started easily and learn later
  - some settings can be configured using standard relay management tools
  - everything kept in a JSON file that can be edited manually

<br clear="all">

- **easy theming options**
  - default looks with dark/light toggle by default
  - but as the relay owner you can opt out of that and pick some crazy colors
  - theme colors are forced upon whoever is visiting the webpages

<div align="center"><img width="600" src="https://github.com/user-attachments/assets/f6986613-faa7-4857-a447-ad4ed2d8a8ef" /></div>
<div align="center"><img width="600" src="https://github.com/user-attachments/assets/a618f2ce-96b2-4e2d-a4b9-ad2876aedd41" /></div>
<div align="center"><img width="600" src="https://github.com/user-attachments/assets/ae238bcf-6908-49af-adad-52455871b074" /></div>

- **paywall functionality**
  - a special hashtag, amount (in satoshis) and period (in days) can be configured
  - notes published by members with the `"-"` tag and the special hashtag are marked as "paid"
  - these notes will only be shown to viewers who have zapped the specific member at least the specified amount in the past specified days
  - normal zaps and nutzaps supported, sourced from the _inbox_ relay
