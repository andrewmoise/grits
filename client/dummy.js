({
    'execute': async (context, prevValue, argsArray) => {
        console.log("Executing dummy function with args:", argsArray);

        if (argsArray.length === 0) {
            return [0];
        }

        return [argsArray[0]+1];
    },
    'name': 'dummy',
    'description': 'Dummy function for testing purposes',
    'versionDate': '2024-02-16',
    'author': 'Developer Name',
    'email': 'developer@example.com'
})
