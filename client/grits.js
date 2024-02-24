

/*****
 Example Usage:

 const gritsStore = new Context('https://localhost:17871/grits/v1/');
 
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

 */


 export class Context {
    constructor(root) {
        this.root = root;
    }

    async load(path) {
        const url = `${this.root}${path}`;
        console.log(`load for ${url}`);

        const response = await fetch(url);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.arrayBuffer(); // Return the response body as an ArrayBuffer
    }

    async save(path, bytearray) {
        const url = `${this.root}${path}`;
        console.log(`save for ${url}`);
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
        console.log(`ref for ${url}`);
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

    async tree() {
        const treeUrl = `${this.root}tree`;
        const treeResponse = await fetch(treeUrl);
        if (!treeResponse.ok) {
            throw new Error(`HTTP error while fetching tree: status ${treeResponse.status}`);
        }
        const treeData = await treeResponse.json();
        const treeHash = treeData.tree; // Assuming the JSON has a 'tree' field with the hash
    
        const blobUrl = `${this.root}blob/${treeHash}`;
        const blobResponse = await fetch(blobUrl);
        if (!blobResponse.ok) {
            throw new Error(`HTTP error while fetching blob: status ${blobResponse.status}`);
        }
        const blobData = await blobResponse.json(); // Assuming the blob data is JSON formatted
    
        return blobData; // Return the parsed JSON
    }    
}
