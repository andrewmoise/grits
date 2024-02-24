({
    'execute': async (job, prevValue, argsArray) => {
        console.log("Executing ls function with args:", argsArray);

        if (prevValue !== null && prevValue !== undefined) {
            throw new Error(".ls() takes no input");
        }

        try {
            console.log("Executing .ls()");
            console.log(`  shell is ${job.shell}`);
            console.log(` shell.context is ${job.shell.context}`)
            console.log(` shell.context.tree is ${job.shell.context.tree}`)
 
            const tree = await job.shell.context.tree(); // Fetch the file tree
            const filenames = Object.keys(tree); // Extract filenames (keys) from the tree
            console.log("Files:", filenames);
            return filenames; // Return the list of filenames
        } catch (error) {
            console.error("Error executing .ls():", error);
            throw error;
        }
    },
    'name': 'ls',
    'description': 'List files',
    'versionDate': '2024-02-22'
})
