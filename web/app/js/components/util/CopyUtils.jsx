import React from 'react';

/*
* Instructions for adding resources to service mesh
*/
export const incompleteMeshMessage = name => {
  if (name) {
    return (
      <div className="action">Add {name} to the k8s.yml file<br /><br />
      Then run <code>conduit inject k8s.yml | kubectl apply -f -</code> to add it to the service mesh</div>
    );
  } else {
    return (
      <div className="action">Add one or more resources to the k8s.yml file<br /><br />
      Then run <code>conduit inject k8s.yml | kubectl apply -f -</code> to add them to the service mesh</div>
    );
  }
};
