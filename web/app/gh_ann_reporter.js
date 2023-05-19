class GhAnnReporter {
  /* eslint-disable class-methods-use-this */
  onRunComplete(contexts, results) {
    // only pick the first line of the error
    const msgRe = /^(.+)$/m;
    const pathRe = /^.+\/linkerd2\/(.+)$/;
    results.testResults.forEach(item => {
      if (item.numFailingTests > 0) {
        const msgArr = msgRe.exec(item.failureMessage);
        let msg = 'Unknown';
        if (msgArr.length > 0) {
          msg = msgArr[1];
        }
        const pathArr = pathRe.exec(item.testFilePath);
        let path = 'Unknown';
        if (pathArr.length > 0) {
          path = pathArr[1];
        }
        /* eslint-disable no-console */
        console.log(`::error file=${path}::${msg}`);
      }
    });
  }
}

module.exports = GhAnnReporter;
