import Button from '@material-ui/core/Button';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';
import PropTypes from 'prop-types';
import React from 'react';
import { withTranslation } from 'react-i18next';

class NamespaceConfirmationModal extends React.Component {

  render() {
    const { open, selectedNamespace, newNamespace, handleConfirmNamespaceChange, handleDialogCancel, t } = this.props;

    return (
      <React.Fragment>
        <Dialog
          open={open}
          onClose={this.handleClose}>
          <DialogTitle>
            {t("Change namespace?")}
          </DialogTitle>
          <DialogContent>
            <DialogContentText>
              {t("message1", { selectedNamespace: selectedNamespace, newNamespace: newNamespace })}
            </DialogContentText>
          </DialogContent>
          <DialogActions>
            <Button onClick={handleConfirmNamespaceChange} color="primary">
              {t("Yes")}
            </Button>
            <Button onClick={handleDialogCancel} variant="text">
              {t("No")}
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
  selectedNamespace: PropTypes.string.isRequired,
  t: PropTypes.func.isRequired,
};

export default withTranslation(["namespaceConfirmationModal", "common"])(NamespaceConfirmationModal);
