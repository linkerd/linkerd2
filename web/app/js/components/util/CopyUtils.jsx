import React from 'react';

/*
* Instructions for adding deployments to service mesh
*/
export const incompleteMeshMessage = name => {
  if (name) {
    return (
      <div className="action">Add {name} to the deployment.yml file<br /><br />
      Then run <code>conduit inject deployment.yml | kubectl apply -f -</code> to add it to the service mesh</div>
    );
  } else {
    return (
      <div className="action">Add one or more deployments to the deployment.yml file<br /><br />
      Then run <code>conduit inject deployment.yml | kubectl apply -f -</code> to add them to the service mesh</div>
    );
  }
};
