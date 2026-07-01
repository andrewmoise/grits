# A framework for web-native development

Gimbal is a little framework for civilized development and hosting of web applications. Some of the things it allows are:

* For end-users, to **access their own data** stored by web app they're using, directly and without needing to go through the designated frontend
* For end-users, to **modify the code of a web app they're using**, if the existing functioning doesn't suit
* For developers, to **clone, modify, or rehost someone else's app**, or "self-host" an app on someone else's server without needing any particular backend setup
* For admins, to **run a lean backend**, with the bulk of the load of the app carried by the browser

The system that accomplishes this is two cooperating pieces:

* **Grits** is the backend. It is, more or less, a networked filesystem oriented around cached and access-controlled content, and a web server for authenticated access to that content.
* **Gimbal** is a little development environment which runs atop Grits. It's designed to create apps which are defined entirely in the browser, and are inspectable and controllable by the end-users running them.

Note this is all still a work in progress. **This code is not production ready or close to it.** Some things work in useful form, but also it's still heavily in flux. It's a hobby project. It works on my machine except when it doesn't. This is mostly just a demo of the intended direction of the system, for people to mess with and give feedback on.

That said, if you want a quick online demo of the system's current capabilities, see [DEMO.md](DEMO.md). If you want to set up your own backend instance, see [INSTALL.md](INSTALL.md). For a detailed reference and explanation of how all this works, see [REFERENCE.md](REFERENCE.md).

Cheers and enjoy.

## Links

Live instance: [https://gimbal.melanic.org/](https://gimbal.melanic.org/)

Matrix: `#gimbal:matrix.org`

Comments, questions, feedback? [Let me know.](mailto:moise@melanic.org)
