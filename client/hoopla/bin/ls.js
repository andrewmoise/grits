({
    'execute': async (job, prevValue, argsArray) => {
        console.log("Executing ls function with args:", argsArray);

        if (prevValue !== null && prevValue !== undefined) {
            throw new Error(".ls() takes no input");
        }

        try {
            const tree = await job.shell.storage.tree(); // Fetch the file tree
            return tree
        } catch (error) {
            console.error("Error executing .ls():", error);
            throw error;
        }
    },
    'name': 'ls',
    'description': 'List files',
    'versionDate': '2024-02-22'
})
