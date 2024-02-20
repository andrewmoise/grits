/*****
 Example Usage:

 ```javascript
let gritsStore;
let context;
 import('/grits/v1/home/root/bin/hoopla.js').then(module => {
    // Now you can use GritsStore and Context
    gritsStore = new module.GritsStore('http://localhost:1787/grits/v1/');
    context = new module.Context(gritsStore);

    // Use context as needed
}).catch(console.error);

context.dummy(1);


// Dynamically import the module and use top-level await
const hooplaModule = await import('https://localhost:1787/grits/v1/client/hoopla.js');

// Assuming GritsStore is initialized or imported elsewhere
const gritsStore = new GritsStore('https://localhost:1787/grits/v1/');

// Instantiate a Context object
const context = new hooplaModule.Context(gritsStore);

// Now you can use the context

context.fetch('https://html5up.net/solid-state/download').unzip().put('solid-state');

```

*/

export class Context {
    constructor(gritsStore) {
        this.gritsStore = gritsStore; // Store the GritsStore instance
        this.commandCache = new Map(); // Cache for command maps, keyed by hash
        return createContextProxy(this); // Automatically return a proxied version of Context
    }

    async loadFunction(functionName) {
        try {
            // Retrieve the hash reference for the function
            const hash = await this.gritsStore.ref(`file/bin/${functionName}.js`);
            if (this.commandCache.has(hash)) {
                // If the command is already cached, return the execute function
                return this.commandCache.get(hash).execute;
            }

            // If not in cache, fetch the actual function code by hash
            const codeArrayBuffer = await this.gritsStore.get(`blob/${hash}`);
            const codeText = new TextDecoder().decode(codeArrayBuffer);
            // Assume codeText evaluates to a command object
            const command = eval(codeText);

            // Cache the loaded command
            this.commandCache.set(hash, command);
            return command.execute;
        } catch (error) {
            console.error(`Failed to load command ${functionName}:`, error);
            throw error;
        }
    }
}

function createContextProxy(context) {
    return new Proxy(context, {
        get(target, prop, receiver) {
            if (prop in target) {
                return target[prop]; // Return the property directly
            } else {
                // If the method is unknown, return a function that initializes a new Job
                return (...args) => {
                    const job = new Job(target);
                    job.addStep(prop, args);
                    job.start();
                    return job;
                };
            }
        }
    });
}

export class Job {
    constructor(context) {
        this.context = context;
        this.steps = []; // To hold [functionName, argsArray] pairs
        this.ready = false; // Flag to indicate readiness to execute

        return createJobProxy(this); // Automatically return a proxied version of Job
    }

    // Method to add a step to the job
    addStep(functionName, argsArray) {
        console.log("Adding step to ", this.steps, ":", functionName, argsArray)
        this.steps.push([functionName, argsArray]);
        return this; // Allow chaining
    }

    // Method to execute the job
    async execute() {
        // Wait for the job to be ready
        while (!this.ready) {
            await new Promise(resolve => setTimeout(resolve, 0));
        }

        let prevValue = null;

        while (this.steps.length > 0) {
            // Take the first step from the queue
            const [functionName, argsArray] = this.steps.shift();
            const func = await this.context.loadFunction(functionName);
            // Execute the function with the provided arguments
            prevValue = await func(this, prevValue, argsArray);
        }

        return prevValue; // Return the result of the last executed function
    }

    // Placeholder method to set the job as ready and start execution
    start() {
        this.execute().then(result => {
            console.log("Job completed with result:", result);
        }).catch(error => {
            console.error("Job execution failed:", error);
        });
        this.ready = true;
    }
}

function createJobProxy(job) {
    return new Proxy(job, {
        get(target, prop, receiver) {
            if (prop in target) {
                return target[prop]; // Return the property directly
            } else {
                // Handle dynamic step addition
                return (...args) => {
                    target.addStep(prop, args);
                    return receiver; // Return the proxy for chaining
                };
            }
        }
    });
}

/*****
 Example Usage:

 ```javascript
 const gritsStore = new GritsStore('https://localhost:17871/grits/v1/');
 
 // Get a bytestream
 gritsStore.get('some/path/file.txt')
   .then(data => {
       console.log(data); // ArrayBuffer of file content
   })
   .catch(console.error);
 
 // Put a bytestream
 const byteArray = new Uint8Array([/your data here *\/]).buffer;
 gritsStore.put('some/path/file.txt', byteArray)
   .then(response => {
       console.log(response); // Response from the PUT request
   })
   .catch(console.error);
 
 // Get a reference hash
 gritsStore.ref('some/path/file.txt')
   .then(hash => {
       console.log(hash); // Hash part of the URL
   })
   .catch(console.error);
 ```

 */


export class GritsStore {
    constructor(root) {
        this.root = root;
    }

    async get(path) {
        const url = `${this.root}${path}`;
        console.log(`get for ${url}`)

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.arrayBuffer(); // Return the response body as an ArrayBuffer
    }

    async put(path, bytearray) {
        const url = `${this.root}${path}`;
        console.log(`put for ${url}`)
        const response = await fetch(url, {
            method: 'PUT',
            body: bytearray, // Assuming bytearray is an instance of ArrayBuffer or similar
            headers: {
                'Content-Type': 'application/octet-stream', // or the correct MIME type
            },
        });
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.text(); // Return the response text, or you might want to return something else
    }

    async ref(path) {
        const url = `${this.root}${path}`;
        console.log(`ref for ${url}`)
        const response = await fetch(url);

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        // Extract the hash part from the redirect URL
        const hashMatch = response.url.match(/\/blob\/([^\/]+)$/);
        if (!hashMatch) {
            throw new Error('Failed to extract hash from redirect URL');
        }
        return hashMatch[1]; // Return the hash part of the URL
    }
}
