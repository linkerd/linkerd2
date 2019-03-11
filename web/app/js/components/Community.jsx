import Iframe from 'react-iframe';
import React from 'react';

function Community() {
  return (
    <React.Fragment>
      <Iframe
        url="https://linkerd.io/dashboard/"
        position="inherit"
        display="block"
        height="100vh"
        border="none" />
    </React.Fragment>
  );
}

export default Community;
