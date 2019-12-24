import PropTypes from 'prop-types';
import React from 'react';
import { friendlyTitle } from './util/Utils.js';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';

const AddResources = ({ resourceName, resourceType }) => (
  <div className="mesh-completion-message">
    {friendlyTitle(resourceType).singular} {resourceName} is not in the mesh.
    {incompleteMeshMessage()}
  </div>
);

AddResources.propTypes = {
  resourceName: PropTypes.string.isRequired,
  resourceType: PropTypes.string.isRequired
};

export default AddResources;
