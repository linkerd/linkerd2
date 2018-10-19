import React from 'react';
import Typography from '@material-ui/core/Typography';

/*
* Instructions for adding resources to service mesh
*/
export const incompleteMeshMessage = name => {
  if (name) {
    return (
      <Typography>
        Add {name} to the k8s.yml file<br /><br />
        Then run <code>linkerd inject k8s.yml | kubectl apply -f -</code> to add it to the service mesh
      </Typography>
    );
  } else {
    return (
      <Typography>
        Add one or more resources to the k8s.yml file<br /><br />
        Then run <code>linkerd inject k8s.yml | kubectl apply -f -</code> to add them to the service mesh
      </Typography>
    );
  }
};
