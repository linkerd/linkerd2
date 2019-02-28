import Iframe from 'react-iframe';
import React from 'react';

function Community() {
  return (
    <React.Fragment>
      <Iframe
        url="https://linkerd.io/dashboard/"
        id="myId"
        className="myClassname"
        position="inherit"
        display="block"
        height="-webkit-fill-available"
        width="-webkit-fill-available"
        border="none" />
    </React.Fragment>
  );
}

export default Community;
