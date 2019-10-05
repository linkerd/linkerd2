import Button from '@material-ui/core/Button';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';
import PropTypes from 'prop-types';
import React from 'react';

class NamespaceConfirmationModal extends React.Component {

  render() {
    const { open, selectedNamespace, newNamespace, handleConfirmNamespaceChange, handleDialogCancel } = this.props;

    return (
      <React.Fragment>
        <Dialog
          open={open}
          onClose={this.handleClose}>
          <DialogTitle>
            Change namespace?
          </DialogTitle>
          <DialogContent>
            <DialogContentText>
              The resource you are viewing is in a different namespace than the namespace you have selected. Do you want to change the namespace from { selectedNamespace } to { newNamespace }?
            </DialogContentText>
          </DialogContent>
          <DialogActions>
            <Button onClick={handleConfirmNamespaceChange} color="primary">
              Yes
            </Button>
            <Button onClick={handleDialogCancel} variant="text">
              No
            </Button>
          </DialogActions>
        </Dialog>
      </React.Fragment>
    );
  }
}

NamespaceConfirmationModal.propTypes = {
  handleConfirmNamespaceChange: PropTypes.func.isRequired,
  handleDialogCancel: PropTypes.func.isRequired,
  newNamespace: PropTypes.string.isRequired,
  open: PropTypes.bool.isRequired,
  selectedNamespace: PropTypes.string.isRequired
};

export default NamespaceConfirmationModal;
