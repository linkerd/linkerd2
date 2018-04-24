import React from 'react';

/*
* Instructions for adding deployments to service mesh
*/
export const incompleteMeshMessage = (name, resource = "deployment") => {
  if (name) {
    return (
      <div className="action">Add {name} to the k8s.yml file<br /><br />
      Then run <code>conduit inject k8s.yml | kubectl apply -f -</code> to add it to the service mesh</div>
    );
  } else {
    return (
      <div className="action">Add one or more {resource}s to the k8s.yml file<br /><br />
      Then run <code>conduit inject k8s.yml | kubectl apply -f -</code> to add them to the service mesh</div>
    );
  }
};
