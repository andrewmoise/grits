# A framework for web-native development

This is a little framework for civilized hosting and deployment of web applications. Some of the things it allows are:

* For users to audit or modify their data stored by the web app without needing to go through the designated frontend
* For ordinary users to modify for themselves a web app they're using, if the existing functioning doesn't suit their wants
* For developers to clone, modify, and/or rehost a web app, or "self-host" a web app on someone else's server without needing any particular backend setup
* For admins and their friends to likewise easily move apps between VPS hosts
* For admins to run a lean backend that can support a wide variety of frontend apps without needing to take on big computational load and database/docker setup for each one

The system that accomplishes this is two cooperating pieces:

* **Grits** is the backend. It is, more or less, a networked filesystem oriented around cached and access-controlled content, and a web server for comfortably serving that content.
* **Gimbal** is a little development environment which runs atop Grits. It's designed to create apps which are transparent and controllable by the end-users running them.

Note this is all still a work in progress. **This code is not production ready or close to it.** Some things work in useful form, but also it's still heavily in flux. It's a hobby project. It works on my machine except when it doesn't. This is mostly just a demo of the intended direction of the system, for people to mess with and give feedback on.

That said, if you want a quick online demo of the system's current capabilities, see [DEMO.md](DEMO.md). If you want to set up your own backend instance, see [INSTALL.md](INSTALL.md). For a detailed reference and explanation of how all this works, see [REFERENCE.md](REFERENCE.md).

Cheers and enjoy.

## Links

Live instance: [https://gimbal.melanic.org/](https://gimbal.melanic.org/)

Matrix: `#gimbal:matrix.org`

Comments, questions, feedback? [Let me know.](mailto:moise@melanic.org)
