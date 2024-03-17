/*****
 Example Usage:

const gritsStore = new Storage('https://localhost:1787/grits/v1/');

// Get a File object, then fetch its blob content
gritsStore.file('some/path/file.txt')
    .then(file => {
        console.log(file.toString()); // Prints file details
        return gritsStore.blob(file.hash); // Fetch the blob content
    })
    .then(blob => {
        // Handle the blob content, e.g., reading it into an ArrayBuffer
        const reader = blob.getReader();
        // Example of reading the stream; adjust as needed
        reader.read().then(({done, value}) => {
            if (!done) {
                console.log(value); // Process chunk
            }
        });
    })
    .catch(console.error);

// Get a Tree object
gritsStore.tree()
    .then(tree => {
        const file = tree.getFile('file.txt');
        if (file) {
            console.log(file.toString()); // Prints details of the found file
        }
    })
    .catch(console.error);

// Get a blob directly by its address
gritsStore.blob('someBlobAddress')
    .then(stream => {
        // Handle the ReadableStream
        console.log(stream); // Placeholder; use stream according to your needs
    })
    .catch(console.error);

 */

export class Storage {
    constructor(root) {
        this.root = root;
        this.currentTreeFileAddr = undefined;
        this.currentTree = undefined;
        this.pathToFile = undefined;
        this.refreshPromise = null; // Track ongoing refresh operation
    }

    async refresh() {
        if (this.refreshPromise) {
            return this.refreshPromise; // Wait for the ongoing refresh to complete
        }
        this.refreshPromise = (async () => {
            try {
                const response = await fetch(`${this.root}tree`);
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                const treeData = await response.json();
                if (this.currentTreeFileAddr !== treeData.tree) {
                    this.currentTreeFileAddr = treeData.tree;
                    const blobResponse = await fetch(`${this.root}blob/${treeData.tree}`);
                    if (!blobResponse.ok) {
                        throw new Error(`HTTP error! status: ${blobResponse.status}`);
                    }
                    this.currentTree = await blobResponse.json();
                    this.buildPathToFile(this.currentTree);
                }
            } catch (error) {
                console.error("Error during refresh:", error);
                throw error; // Re-throw error after logging
            } finally {
                this.refreshPromise = null; // Reset the promise when done
            }
        })();
        return this.refreshPromise;
    }

    buildPathToFile(blobData) {
        this.pathToFile = {};
        this.currentTree = Object.setPrototypeOf(blobData, TreePrototype);

        blobData.children.forEach(child => {
            if (child.name in this.pathToFile) {
                // Invalidate the cache and throw an error
                this.pathToFile = undefined;
                this.currentTree = undefined;
                this.currentTreeFileAddr = undefined;

                throw new Error(`Duplicate filename found for path: ${child.name}`);
            }
            this.pathToFile[child.name] = child;
        });
    }
    
    async applyPrototype(data, prototype, requiredFields = []) {
        // Validate required fields
        for (const field of requiredFields) {
            if (!(field in data)) {
                throw new Error(`Missing required field: ${field}`);
            }
        }
        // Apply the prototype
        return Object.setPrototypeOf(data, prototype);
    }

    async file(path) {
        if (this.pathToFile === undefined) {
            await this.refresh(); // Populate the cache if empty
        }
        const fileData = this.pathToFile[path];
        if (!fileData) {
            throw new Error(`File not found: ${path}`);
        }
        return this.applyPrototype({...fileData, path}, FilePrototype, ['hash', 'name', 'type']);
    }

    async tree() {
        if (this.pathToFile === undefined) {
            await this.refresh(); // Populate the cache if empty
        }
        // This method might need to fetch or use cached tree data
        return this.currentTree;
    }

    async stream(path) {
        const file = await this.file(path);
        return this.blob(file.hash);
    }

    async bytearray(path) {
        const stream = await this.stream(path);
        const reader = stream.getReader();
        const chunks = [];
        let done, value;
        while (!({ done, value } = await reader.read()).done) {
            chunks.push(value);
        }
        const totalLength = chunks.reduce((acc, val) => acc + val.length, 0);
        let result = new Uint8Array(totalLength);
        let position = 0;
        for (let chunk of chunks) {
            result.set(chunk, position);
            position += chunk.length;
        }
        return result;
    }

    async blob(fileAddr) {
        const response = await fetch(`${this.root}blob/${fileAddr}`);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.body; // Returns a ReadableStream of the body contents
    }

    async upload(blobData) {
        const response = await fetch(`${this.root}upload`, {
            method: 'POST',
            body: blobData,
        });
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        const newResourceAddr = await response.text(); // Assuming the response is a JSON-encoded bare string
        return newResourceAddr;
    }
}
    
const FilePrototype = {
    toString() {
        return `${this.name} - ${this.hash}`;
    }
};

const TreePrototype = {
};
