/*****
 Example Usage:

let hoo;

import('/grits/v1/file/client/grits.js').then(module => {
    import('/grits/v1/file/client/hoopla.js').then(hooplaModule => {
        const storage = new module.Storage('http://localhost:1787/grits/v1/');
        hoo = new hooplaModule.Shell(storage);
    }).catch(console.error);
}).catch(console.error);

hoo.dummy(1);

// Ultimate WIP goal:

hoo.fetch('https://html5up.net/solid-state/download').unzip().put('solid-state');


*/


export class Shell {
    constructor(storage) {
        this.storage = storage;
        this.commandCache = new Map(); // Cache for command maps, keyed by hash
        this.jobs = []
        return createShellProxy(this); // Automatically return a proxied version of Shell
    }

    async loadFunction(functionName) {
        try {
            const codeRequest = await fetch(`http://localhost:1787/grits/v1/file/client/bin/${functionName}.js`);
            if (!codeRequest.ok) {
                throw new Error(`HTTP error, status = ${codeRequest.status}`);
            }

            const codeText = await codeRequest.text();
            const command = eval(codeText);
            
            return command.execute;
        } catch (error) {
            console.error(`Failed to load command ${functionName}:`, error);
            throw error;
        }
    }

    async loadFunctionFromGrits(functionName) {
        try {
            // Retrieve the hash reference for the function
            const hash = await this.storage.ref(`file/bin/${functionName}.js`);
            if (this.commandCache.has(hash)) {
                // If the command is already cached, return the execute function
                return this.commandCache.get(hash).execute;
            }

            // If not in cache, fetch the actual function code by hash
            const codeArrayBuffer = await this.storage.load(`blob/${hash}`);
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

    r(index) {
        return this.jobs[index].prevValue;
    }
}

function createShellProxy(shell) {
    return new Proxy(shell, {
        get(target, prop, receiver) {
            if (prop === 'toString') {
                return () => `Shell Proxy with ${target.jobs.length} jobs`;
            } else if (prop === Symbol.toPrimitive) {
                return (hint) => hint === "string" ? `Shell Proxy: ${target.jobs.length} jobs` : undefined;
            } else if (prop in target) {
                return typeof target[prop] === 'function' ? target[prop].bind(target) : target[prop];
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

const JobState = {
    NOT_STARTED: 'not started',
    RUNNING: 'running',
    ERROR: 'error',
    ABORTED: 'aborted',
    DONE: 'done',
};

export class Job {
    constructor(shell) {
        this.shell = shell;
        this.steps = []; // To hold [functionName, argsArray] pairs
        this.state = JobState.NOT_STARTED;
        this.promise = null; // Promise to hold the result of the job

        this.jobIndex = shell.jobs.length;
        shell.jobs.push(this);

        return createJobProxy(this); // Automatically return a proxied version of Job
    }

    // Method to add a step to the job
    addStep(functionName, argsArray) {
        console.log("Adding step to ", this.steps, ":", functionName, argsArray);
        this.steps.push([functionName, argsArray]);
        return this; // Allow chaining
    }

    // Method to execute the job
    async execute() {
        // Wait for the job to be ready
        while (this.state == JobState.NOT_STARTED) {
            await new Promise(resolve => setTimeout(resolve, 0));
        }

        console.log(`[${this.jobIndex}] ${this.steps.map(j => j[0]).join(' ')}`);

        let prevValue = null;

        try {
            while (this.steps.length > 0) {
                // Take the first step from the queue
                const [functionName, argsArray] = this.steps.shift();
                const func = await this.shell.loadFunction(functionName);
                // Execute the function with the provided arguments
                prevValue = await func(this, prevValue, argsArray);
            }
            if (prevValue !== null) {
                console.log(prevValue);
            }
            console.log(`[${this.jobIndex}] Done`);
            this.state = JobState.DONE;
        } catch (error) {
            console.error(`[${this.jobIndex}] Error:`, error);
            this.state = JobState.ERROR;
            throw error;
        }

        return prevValue; // Return the result of the last executed function
    }

    // Placeholder method to set the job as ready and start execution
    start() {
        (this.promise = this.execute()).then(result => {
            console.log("Job completed with result:", result);
        }).catch(error => {
            console.error("Job execution failed:", error);
        });
        this.state = JobState.RUNNING;
    }

    // Returns the promise of the job's completion
    p() {
        return this.promise;
    }

    // Await the result of the job
    async r() {
        return await this.promise;
    }
}

function createJobProxy(job) {
    return new Proxy(job, {
        get(target, prop, receiver) {
            if (prop === 'toString') {
                return () => `Job Proxy #${target.jobIndex}: ${target.state} with ${target.steps.length} steps`;
            } else if (prop === Symbol.toPrimitive) {
                return (hint) => hint === "string" ? `Job Proxy #${target.jobIndex}: ${target.state}, ${target.steps.length} steps` : undefined;
            } else if (prop in target) {
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
